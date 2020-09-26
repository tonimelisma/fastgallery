package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cheggaaa/pb/v3"
	"github.com/h2non/bimg"
)

// global defaults
var optSymlinkDir = "Original"
var optFullsizeDir = "Pictures"
var optThumbnailDir = "Thumbnails"

var optDirectoryMode os.FileMode = 0755
var optFileMode os.FileMode = 0644

var thumbnailExtension = ".jpg"
var fullsizePictureExtension = ".jpg"
var fullsizeVideoExtension = ".mp4"

// this function parses command-line arguments
func parseArgs() (inputDirectory string, outputDirectory string, optDryRun bool) {
	outputDirectoryPtr := flag.String("o", ".", "Output root directory for gallery")
	optDryRunPtr := flag.Bool("d", false, "Dry run - don't make changes, only explain what would be done")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [OPTION]... DIRECTORY\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Create a static photo and video gallery from all\nsubdirectories and files in directory.\n")
		fmt.Fprintf(os.Stderr, "\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	if flag.NArg() == 0 {
		fmt.Fprintf(os.Stderr, "%s: missing directories to use as input\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Try '%s -h' for more information.\n", os.Args[0])
		os.Exit(1)
	}

	if flag.NArg() != 1 {
		fmt.Fprintf(os.Stderr, "%s: wrong number of arguments given for input (expected one)\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Try '%s -h' for more information.\n", os.Args[0])
		os.Exit(1)
	}

	if _, err := os.Stat(flag.Args()[0]); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "%s: Directory does not exist: %s\n", os.Args[0], flag.Args()[0])
		os.Exit(1)
	}

	if _, err := os.Stat(*outputDirectoryPtr); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "%s: Directory does not exist: %s\n", os.Args[0], *outputDirectoryPtr)
		os.Exit(1)
	}

	if isEmptyDir(flag.Args()[0]) {
		fmt.Fprintf(os.Stderr, "%s: Input directory is empty: %s\n", os.Args[0], flag.Args()[0])
		os.Exit(1)
	}

	_, err := exec.LookPath("ffmpeg")
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: Can't find ffmpeg in path\n", os.Args[0])
		os.Exit(1)
	}

	// add a parameter flag for this? warnings are useless as they don't provide the filename
	// P.S. who the hell creates a library that pushes warnings to the console by default!?
	os.Setenv("VIPS_WARNING", "0")

	return *outputDirectoryPtr, flag.Args()[0], *optDryRunPtr
}

// each file has a corresponding struct with relative and absolute paths
// for source files, if a newer thumbnail exists in gallery we set the existing flag and don't copy it
// for gallery files, if no corresponding source file exists, the existing flag stays false
// all non-existing gallery files will be deleted in the end
type file struct {
	name    string
	relPath string
	absPath string
	modTime time.Time
	exists  bool
}

type directory struct {
	name           string
	relPath        string
	absPath        string
	modTime        time.Time
	subdirectories []directory
	files          []file
}

func checkError(e error) {
	if e != nil {
		panic(e)
	}
}

func isEmptyDir(directory string) (isEmpty bool) {
	// TODO figure out a faster way to check if directory is empty
	list, err := ioutil.ReadDir(directory)
	checkError(err)

	if len(list) == 0 {
		return true
	}
	return false
}

func isVideoFile(filename string) bool {
	switch filepath.Ext(strings.ToLower(filename)) {
	case ".mp4", ".mov", ".3gp", ".avi", ".mts", ".m4v", ".mpg":
		return true
	default:
		return false
	}
}

func isImageFile(filename string) bool {
	switch filepath.Ext(strings.ToLower(filename)) {
	case ".jpg", ".jpeg", ".heic", ".png", ".gif", ".tif":
		return true
	case ".cr2", ".raw", ".arw":
		return true
	default:
		return false
	}
}

func isMediaFile(filename string) bool {
	if isImageFile(filename) || isVideoFile(filename) {
		return true
	}
	return false
}

func recurseDirectory(thisDirectory string, relativeDirectory string) (root directory) {
	root.name = filepath.Base(thisDirectory)
	asIsStat, _ := os.Stat(thisDirectory)
	root.modTime = asIsStat.ModTime()
	root.relPath = relativeDirectory
	root.absPath, _ = filepath.Abs(thisDirectory)

	list, err := ioutil.ReadDir(thisDirectory)
	checkError(err)

	for _, entry := range list {
		if entry.IsDir() {
			if !isEmptyDir(filepath.Join(thisDirectory, entry.Name())) {
				root.subdirectories = append(root.subdirectories, recurseDirectory(filepath.Join(thisDirectory, entry.Name()), filepath.Join(relativeDirectory, root.name)))
			}
		} else {
			if isMediaFile(entry.Name()) {
				root.files = append(root.files, file{name: entry.Name(), modTime: entry.ModTime(), relPath: filepath.Join(relativeDirectory, root.name, entry.Name()), absPath: filepath.Join(thisDirectory, entry.Name()), exists: false})
			}
		}
	}

	return (root)
}

func stripExtension(fullFilename string) (baseFilename string) {
	extension := filepath.Ext(fullFilename)
	return fullFilename[0 : len(fullFilename)-len(extension)]
}

func compareDirectories(source *directory, gallery *directory) {
	for i, inputFile := range source.files {
		inputFileBasename := stripExtension(inputFile.name)
		for j, outputFile := range gallery.files {
			outputFileBasename := stripExtension(outputFile.name)
			if inputFileBasename == outputFileBasename {
				gallery.files[j].exists = true
				if !outputFile.modTime.Before(inputFile.modTime) {
					source.files[i].exists = true
				}
			}
		}
	}

	for k, inputDir := range source.subdirectories {
		for l, outputDir := range gallery.subdirectories {
			if inputDir.name == outputDir.name {
				compareDirectories(&(source.subdirectories[l]), &(gallery.subdirectories[k]))
			}
		}
	}
}

func countChanges(source directory) (outputChanges int) {
	outputChanges = 0
	for _, file := range source.files {
		if !file.exists {
			outputChanges++
		}
	}

	for _, dir := range source.subdirectories {
		outputChanges = outputChanges + countChanges(dir)
	}

	return outputChanges
}

func createGallery(source directory, sourceRootDir string, gallery directory, progressBar *pb.ProgressBar, optDryRun bool) {
	// Create directories if they don't exist
	symlinkDirectoryPath := strings.Replace(filepath.Join(gallery.absPath, source.relPath, source.name), sourceRootDir, optSymlinkDir, 1)
	createDirectory(symlinkDirectoryPath, optDryRun)

	fullsizeDirectoryPath := strings.Replace(filepath.Join(gallery.absPath, source.relPath, source.name), sourceRootDir, optFullsizeDir, 1)
	createDirectory(fullsizeDirectoryPath, optDryRun)

	thumbnailDirectoryPath := strings.Replace(filepath.Join(gallery.absPath, source.relPath, source.name), sourceRootDir, optThumbnailDir, 1)
	createDirectory(thumbnailDirectoryPath, optDryRun)

	for _, file := range source.files {
		if !file.exists {
			// Symlink each file
			symlinkFilePath := strings.Replace(filepath.Join(gallery.absPath, file.relPath), sourceRootDir, optSymlinkDir, 1)
			symlinkFile(file.absPath, symlinkFilePath, optDryRun)

			// Create full-size copy
			fullsizeFilePath := strings.Replace(filepath.Join(gallery.absPath, file.relPath), sourceRootDir, optFullsizeDir, 1)
			fullsizeCopyFile(file.absPath, fullsizeFilePath, optDryRun)

			// Create thumbnail
			thumbnailFilePath := strings.Replace(filepath.Join(gallery.absPath, file.relPath), sourceRootDir, optThumbnailDir, 1)
			thumbnailCopyFile(file.absPath, thumbnailFilePath, optDryRun)

			progressBar.Increment()
		}
	}

	// Recurse into each subdirectory to continue creating symlinks
	for _, dir := range source.subdirectories {
		createGallery(dir, sourceRootDir, gallery, progressBar, optDryRun)
	}
}

func createDirectory(destination string, optDryRun bool) {
	if _, err := os.Stat(destination); os.IsNotExist(err) {
		if optDryRun {
			fmt.Println("Would create dir", destination)
		} else {
			err := os.Mkdir(destination, optDirectoryMode)
			checkError(err)
		}
	}
}

func symlinkFile(source string, destination string, optDryRun bool) {
	if optDryRun {
		fmt.Println("Would link", source, "to", destination)
	} else {
		if _, err := os.Stat(destination); err == nil {
			err := os.Remove(destination)
			checkError(err)
		}
		err := os.Symlink(source, destination)
		checkError(err)
	}
}

func resizeThumbnailVideo(source string, destination string) {
	ffmpegCommand := exec.Command("ffmpeg", "-y", "-i", source, "-ss", "00:00:01", "-vframes", "1", "-vf", "scale=200:200:force_original_aspect_ratio=increase,crop=200:200", "-loglevel", "error", destination)
	ffmpegCommand.Stdout = os.Stdout
	ffmpegCommand.Stderr = os.Stderr

	err := ffmpegCommand.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could create thumbnail of video %s", source)
	}
}

func resizeFullsizeVideo(source string, destination string) {
	ffmpegCommand := exec.Command("ffmpeg", "-y", "-i", source, "-vcodec", "h264", "-acodec", "aac", "-movflags", "faststart", "-vf", "scale='min(640,iw)':'min(640,ih)':force_original_aspect_ratio=decrease", "-crf", "18", "-loglevel", "error", destination)
	ffmpegCommand.Stdout = os.Stdout
	ffmpegCommand.Stderr = os.Stderr

	err := ffmpegCommand.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could create full-size video of %s", source)
	}
}

func resizeThumbnailImage(source string, destination string) {
	buffer, err := bimg.Read(source)
	checkError(err)

	newImage, err := bimg.NewImage(buffer).Thumbnail(200)
	checkError(err)

	newImage2, err := bimg.NewImage(newImage).AutoRotate()

	bimg.Write(destination, newImage2)
}

func resizeFullsizeImage(source string, destination string) {
	buffer, err := bimg.Read(source)
	checkError(err)

	bufferImageSize, err := bimg.Size(buffer)
	ratio := bufferImageSize.Width / bufferImageSize.Height

	newImage, err := bimg.NewImage(buffer).Resize(ratio*1080, 1080)
	checkError(err)

	newImage2, err := bimg.NewImage(newImage).AutoRotate()

	bimg.Write(destination, newImage2)
}

func fullsizeCopyFile(source string, destination string, optDryRun bool) {
	if isImageFile(source) {
		destination = stripExtension(destination) + fullsizePictureExtension
		if optDryRun {
			fmt.Println("Would full-size copy image", source, "to", destination)
		} else {
			resizeFullsizeImage(source, destination)
		}
	} else if isVideoFile(source) {
		destination = stripExtension(destination) + fullsizeVideoExtension
		if optDryRun {
			fmt.Println("Would full-size copy video ", source, "to", destination)
		} else {
			resizeFullsizeVideo(source, destination)
		}
	} else {
		fmt.Println("can't recognize file type for full-size copy", source)
	}
}

func thumbnailCopyFile(source string, destination string, optDryRun bool) {
	if isImageFile(source) {
		destination = stripExtension(destination) + thumbnailExtension
		if optDryRun {
			fmt.Println("Would thumbnail copy image", source, "to", destination)
		} else {
			resizeThumbnailImage(source, destination)
		}
	} else if isVideoFile(source) {
		destination = stripExtension(destination) + thumbnailExtension
		if optDryRun {
			fmt.Println("Would thumbnail copy video ", source, "to", destination)
		} else {
			resizeThumbnailVideo(source, destination)
		}
	} else {
		fmt.Println("can't recognize file type for thumbnail copy", source)
	}
}

func cleanGallery(gallery directory, optDryRun bool) {
	for _, file := range gallery.files {
		if !file.exists {
			if optDryRun {
				fmt.Println("Would delete", file.absPath)
			} else {
				err := os.Remove(file.absPath)
				checkError(err)
			}

		}
	}

	for _, dir := range gallery.subdirectories {
		cleanGallery(dir, optDryRun)
	}

	if isEmptyDir(gallery.absPath) {
		if optDryRun {
			fmt.Println("Would remove empty directory", gallery.absPath)
		} else {
			err := os.Remove(gallery.absPath)
			checkError(err)
		}
	}
}

func main() {
	var inputDirectory string
	var outputDirectory string
	var optDryRun bool

	var gallery directory
	var source directory

	outputDirectory, inputDirectory, optDryRun = parseArgs()

	fmt.Println(os.Args[0], ": Creating photo gallery")
	fmt.Println("")
	fmt.Println("Gathering photos and videos from:", inputDirectory)
	fmt.Println("Creating static gallery in:", outputDirectory)
	if optDryRun {
		fmt.Println("Only dry run, not actually changing anything")
	}
	fmt.Println("")

	// create directory sructs by recursing through source and gallery directories
	gallery = recurseDirectory(outputDirectory, "")
	source = recurseDirectory(inputDirectory, "")

	// check whether gallery already has up-to-date pictures of sources,
	// mark existing pictures in structs
	for _, dir := range gallery.subdirectories {
		if dir.name == optSymlinkDir {
			compareDirectories(&source, &dir)
		}
	}
	for _, dir := range gallery.subdirectories {
		if dir.name == optFullsizeDir {
			compareDirectories(&source, &dir)
		}
	}
	for _, dir := range gallery.subdirectories {
		if dir.name == optThumbnailDir {
			compareDirectories(&source, &dir)
		}
	}

	progressBar := pb.StartNew(countChanges(source))

	// create the gallery
	createGallery(source, source.name, gallery, progressBar, optDryRun)
	progressBar.Finish()

	fmt.Println("Gallery created! Cleaning up...")
	// delete stale pictures
	cleanGallery(gallery, optDryRun)
	fmt.Println("Done!")
}
