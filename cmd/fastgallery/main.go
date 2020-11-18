package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/cheggaaa/pb/v3"
	"github.com/davidbyttow/govips/v2/vips"
)

// global defaults
const optSymlinkDir = "_original"
const optFullsizeDir = "_pictures"
const optThumbnailDir = "_thumbnails"

const optDirectoryMode os.FileMode = 0755
const optFileMode os.FileMode = 0644

const thumbnailExtension = ".jpg"
const fullsizePictureExtension = ".jpg"
const fullsizeVideoExtension = ".mp4"

const thumbnailWidth = 280
const thumbnailHeight = 210
const fullsizeMaxWidth = 1920
const fullsizeMaxHeight = 1080

const videoWorkerPoolSize = 2
const imageWorkerPoolSize = 5

var optIgnoreVideos = false
var optDryRun = false
var optCleanUp = false

// templates
const rawTemplate = `<!DOCTYPE html>
<html lang="en">
 <head>
 <meta charset="utf-8">
<title>{{ .Title }}</title>
<!--<link rel="stylesheet" href="css/style.css">-->
<!--lightbox here-->
 </head>
 <body>
	{{range .Subdirectories}}
	  <a href="{{ .Name }}">
		<div class="icon">
		{{range .Thumbnails}}
		  <img src="{{ . }}" width="50%">
		{{end}}
		</div>
	  </a>
	{{end}}
	{{range .Files}}
	  <a href="{{ .Fullsize }}">
	    <div class="icon">
		  <img src="{{ .Thumbnail }}" original alt="{{ .Original }}">
		</div>
	  </a>
	{{end}}
 </body>
</html>
`

// this function parses command-line arguments
func parseArgs() (inputDirectory string, outputDirectory string) {
	outputDirectoryPtr := flag.String("o", ".", "Output root directory for gallery")
	optIgnoreVideosPtr := flag.Bool("v", false, "Ignore video files")
	optCleanUpPtr := flag.Bool("c", false, "Clean up - delete stale media files from output directory")
	optDryRunPtr := flag.Bool("d", false, "Dry run - don't make changes, only explain what would be done")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [OPTION]... DIRECTORY\n\n", os.Args[0])
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
		fmt.Fprintf(os.Stderr, "Supply all options before input directory (go standard library limitation).\n")
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

	if *optDryRunPtr {
		optDryRun = true
	}

	if *optCleanUpPtr {
		optCleanUp = true
	}

	if *optIgnoreVideosPtr {
		optIgnoreVideos = true
	} else {
		_, err := exec.LookPath("ffmpeg")
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: Can't find ffmpeg in path\n", os.Args[0])
			os.Exit(1)
		}
	}

	return *outputDirectoryPtr, flag.Args()[0]
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

// struct used to fill in data for each html page
type htmlData struct {
	Title          string
	Subdirectories []struct {
		Name       string
		Thumbnails []string
	}
	Files []struct {
		Filename  string
		Thumbnail string
		Fullsize  string
		Original  string
	}
}

// struct used to send jobs to workers via channels
type job struct {
	source      string
	destination string
}

func checkError(e error) {
	if e != nil {
		fmt.Fprintln(os.Stderr, "Error:", e)
	}
}

func isEmptyDir(directory string) (isEmpty bool) {
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
	case ".jpg", ".jpeg", ".heic", ".png", ".gif", ".tif", ".tiff":
		return true
	case ".cr2", ".raw", ".arw":
		return true
	default:
		return false
	}
}

func isMediaFile(filename string) bool {
	if isImageFile(filename) {
		return true
	}

	if !optIgnoreVideos && isVideoFile(filename) {
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

func createGallery(source directory, sourceRootDir string, gallery directory, fullsizeImageJobs chan job, thumbnailImageJobs chan job, fullsizeVideoJobs chan job, thumbnailVideoJobs chan job) {
	// Create directories if they don't exist
	fullsizeDirectoryPath := strings.Replace(filepath.Join(gallery.absPath, source.relPath, source.name), sourceRootDir, optFullsizeDir, 1)
	createDirectory(fullsizeDirectoryPath)

	thumbnailDirectoryPath := strings.Replace(filepath.Join(gallery.absPath, source.relPath, source.name), sourceRootDir, optThumbnailDir, 1)
	createDirectory(thumbnailDirectoryPath)

	symlinkDirectoryPath := strings.Replace(filepath.Join(gallery.absPath, source.relPath, source.name), sourceRootDir, optSymlinkDir, 1)
	createDirectory(symlinkDirectoryPath)

	htmlDirectoryPath := strings.Replace(filepath.Join(gallery.absPath, source.relPath, source.name), sourceRootDir, "", 1)
	createDirectory(htmlDirectoryPath)

	for _, file := range source.files {
		if !file.exists {
			// Create full-size copy
			fullsizeFilePath := strings.Replace(filepath.Join(gallery.absPath, file.relPath), sourceRootDir, optFullsizeDir, 1)
			fullsizeCopyFile(file.absPath, fullsizeFilePath, fullsizeImageJobs, fullsizeVideoJobs)

			// Create thumbnail
			thumbnailFilePath := strings.Replace(filepath.Join(gallery.absPath, file.relPath), sourceRootDir, optThumbnailDir, 1)
			thumbnailCopyFile(file.absPath, thumbnailFilePath, thumbnailImageJobs, thumbnailVideoJobs)

			// Symlink each file
			symlinkFilePath := strings.Replace(filepath.Join(gallery.absPath, file.relPath), sourceRootDir, optSymlinkDir, 1)
			symlinkFile(file.absPath, symlinkFilePath)
		}
	}

	// Recurse into each subdirectory to continue creating symlinks
	for _, dir := range source.subdirectories {
		createGallery(dir, sourceRootDir, gallery, fullsizeImageJobs, thumbnailImageJobs, fullsizeVideoJobs, thumbnailVideoJobs)
	}

	createHTML(source.subdirectories, source.files, sourceRootDir, htmlDirectoryPath)
}

func getHTMLRelPath(originalRelPath string, newRootDir string, sourceRootDir string, folderThumbnail bool) (thumbnailRelPath string) {
	// Calculate relative path to know how many /../ we need to put into URL to get to root of Gallery
	directoryList := strings.Split(originalRelPath, "/")
	// Subtract filename from length
	// HTML files have file thumbnails, pictures and links and folder thumbnails - the latter
	// are one level deeper but linked on the same level, thus the hack below
	var directoryDepth int
	if folderThumbnail {
		directoryDepth = len(directoryList) - 3
	} else {
		directoryDepth = len(directoryList) - 2
	}
	var escapeStringArray []string
	for j := 0; j < directoryDepth; j++ {
		escapeStringArray = append(escapeStringArray, "..")
	}

	return filepath.Join(strings.Join(escapeStringArray, "/"), strings.Replace(originalRelPath, sourceRootDir, newRootDir, 1))
}

func createHTML(subdirectories []directory, files []file, sourceRootDir string, htmlDirectoryPath string) {
	htmlFilePath := filepath.Join(htmlDirectoryPath, "index.html")

	var data htmlData

	data.Title = filepath.Base(htmlDirectoryPath)
	for _, dir := range subdirectories {
		var thumbnails []string
		// Link four first thumbnails to folder image
		for i := 0; i < len(dir.files) && i < 4; i++ {
			thumbnailRelURL := getHTMLRelPath(stripExtension(dir.files[i].relPath)+thumbnailExtension, optThumbnailDir, sourceRootDir, true)
			thumbnails = append(thumbnails, thumbnailRelURL)
		}

		data.Subdirectories = append(data.Subdirectories, struct {
			Name       string
			Thumbnails []string
		}{Name: dir.name, Thumbnails: thumbnails})
	}
	for _, file := range files {
		if isImageFile(file.absPath) {
			data.Files = append(data.Files, struct {
				Filename  string
				Thumbnail string
				Fullsize  string
				Original  string
			}{Filename: file.name, Thumbnail: getHTMLRelPath(stripExtension(file.relPath)+thumbnailExtension, optThumbnailDir, sourceRootDir, false), Fullsize: getHTMLRelPath(stripExtension(file.relPath)+fullsizePictureExtension, optFullsizeDir, sourceRootDir, false), Original: getHTMLRelPath(file.relPath, optSymlinkDir, sourceRootDir, false)})
		} else if isVideoFile(file.absPath) {
			data.Files = append(data.Files, struct {
				Filename  string
				Thumbnail string
				Fullsize  string
				Original  string
			}{Filename: file.name, Thumbnail: getHTMLRelPath(stripExtension(file.relPath)+thumbnailExtension, optThumbnailDir, sourceRootDir, false), Fullsize: getHTMLRelPath(stripExtension(file.relPath)+fullsizeVideoExtension, optFullsizeDir, sourceRootDir, false), Original: getHTMLRelPath(file.relPath, optSymlinkDir, sourceRootDir, false)})
		} else {
			fmt.Println("can't create thumbnail in HTML for file", file.absPath)

		}
	}

	if optDryRun {
		fmt.Println("Would create HTML:", htmlFilePath)
	} else {
		cookedTemplate, err := template.New("index").Parse(rawTemplate)
		checkError(err)

		htmlFileHandle, err := os.Create(htmlFilePath)
		checkError(err)
		defer htmlFileHandle.Close()

		err = cookedTemplate.Execute(htmlFileHandle, data)
		checkError(err)

		htmlFileHandle.Sync()
		htmlFileHandle.Close()
	}
}

func createDirectory(destination string) {
	if _, err := os.Stat(destination); os.IsNotExist(err) {
		if optDryRun {
			fmt.Println("Would create dir", destination)
		} else {
			err := os.Mkdir(destination, optDirectoryMode)
			checkError(err)
		}
	}
}

func symlinkFile(source string, destination string) {
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
	ffmpegCommand := exec.Command("ffmpeg", "-y", "-i", source, "-ss", "00:00:01", "-vframes", "1", "-vf", fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d", thumbnailWidth, thumbnailHeight, thumbnailWidth, thumbnailHeight), "-loglevel", "fatal", destination)
	ffmpegCommand.Stdout = os.Stdout
	ffmpegCommand.Stderr = os.Stderr

	// TODO overlay triangle to thumbnail to implicate it's video instead of image

	err := ffmpegCommand.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could create thumbnail of video %s", source)
	}
}

func resizeFullsizeVideo(source string, destination string) {
	ffmpegCommand := exec.Command("ffmpeg", "-y", "-i", source, "-pix_fmt", "yuv420p", "-vcodec", "libx264", "-acodec", "aac", "-movflags", "faststart", "-r", "24", "-vf", "scale='min(640,iw)':'min(640,ih)':force_original_aspect_ratio=decrease", "-crf", "28", "-loglevel", "fatal", destination)
	ffmpegCommand.Stdout = os.Stdout
	ffmpegCommand.Stderr = os.Stderr

	err := ffmpegCommand.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could create full-size video of %s", source)
	}
}

func resizeThumbnailImage(source string, destination string) {
	if thumbnailExtension == ".jpg" {
		image, err := vips.NewImageFromFile(source)
		checkError(err)

		// TODO fix inefficiency, autorotate before resizing
		// Needed for now to simplify thumbnailing calculations
		err = image.AutoRotate()
		checkError(err)

		// Resize and crop picture to suitable
		ratio := float64(image.Height()) / float64(image.Width())
		targetRatio := float64(thumbnailHeight) / float64(thumbnailWidth)

		if ratio < targetRatio {
			// Picture is wider than thumbnail
			// Resize by height to fit thumbnail size, then crop left and right edge
			scale := float64(thumbnailHeight) / float64(image.Height())
			err = image.Resize(scale, vips.KernelAuto)
			checkError(err)

			// Calculate how much to crop from each edge
			cropAmount := (image.Width() - thumbnailWidth) / 2
			err = image.ExtractArea(cropAmount, 0, thumbnailWidth, thumbnailHeight)
			checkError(err)
		} else if ratio > targetRatio {
			// Picture is higher than thumbnail
			// Resize by width to fit thumbnail size, then crop top and bottom edge
			scale := float64(thumbnailWidth) / float64(image.Width())
			err = image.Resize(scale, vips.KernelAuto)
			checkError(err)

			// Calculate how much to crop from each edge
			cropAmount := (image.Height() - thumbnailHeight) / 2
			err = image.ExtractArea(0, cropAmount, thumbnailWidth, thumbnailHeight)
			checkError(err)
		} else {
			// Picture has same aspect ratio as thumbnail
			// Resize, but no need to crop after resize
			scale := float64(thumbnailWidth) / float64(image.Width())
			err = image.Resize(scale, vips.KernelAuto)
			checkError(err)
		}

		ep := vips.NewDefaultJPEGExportParams()
		imageBytes, _, err := image.Export(ep)
		checkError(err)

		err = ioutil.WriteFile(destination, imageBytes, optFileMode)
		checkError(err)
	} else {
		fmt.Fprintf(os.Stderr, "Can't figure out what format to convert thumbnail image to: %s\n", destination)
	}
}

func resizeFullsizeImage(source string, destination string) {
	if fullsizePictureExtension == ".jpg" {
		image, err := vips.NewImageFromFile(source)
		checkError(err)

		err = image.AutoRotate()
		checkError(err)

		scale := float64(fullsizeMaxWidth) / float64(image.Width())
		if (scale * float64(image.Height())) > float64(fullsizeMaxHeight) {
			scale = float64(fullsizeMaxHeight) / float64(image.Height())
		}

		err = image.Resize(scale, vips.KernelAuto)
		checkError(err)

		ep := vips.NewDefaultJPEGExportParams()
		imageBytes, _, err := image.Export(ep)
		checkError(err)

		err = ioutil.WriteFile(destination, imageBytes, optFileMode)
		checkError(err)
	} else {
		fmt.Fprintf(os.Stderr, "Can't figure out what format to convert full size image to: %s\n", destination)
	}
}

func fullsizeImageWorker(wg *sync.WaitGroup, imageJobs chan job, progressBar *pb.ProgressBar) {
	defer wg.Done()
	for job := range imageJobs {
		resizeFullsizeImage(job.source, job.destination)
		if !optDryRun {
			progressBar.Increment()
		}
	}
}

func fullsizeVideoWorker(wg *sync.WaitGroup, videoJobs chan job, progressBar *pb.ProgressBar) {
	defer wg.Done()
	for job := range videoJobs {
		resizeFullsizeVideo(job.source, job.destination)
		if !optDryRun {
			progressBar.Increment()
		}
	}
}

func fullsizeCopyFile(source string, destination string, fullsizeImageJobs chan job, fullsizeVideoJobs chan job) {
	if isImageFile(source) {
		destination = stripExtension(destination) + fullsizePictureExtension
		if optDryRun {
			fmt.Println("Would full-size copy image", source, "to", destination)
		} else {
			var imageJob job
			imageJob.source = source
			imageJob.destination = destination
			fullsizeImageJobs <- imageJob
		}
	} else if isVideoFile(source) {
		destination = stripExtension(destination) + fullsizeVideoExtension
		if optDryRun {
			fmt.Println("Would full-size copy video", source, "to", destination)
		} else {
			var videoJob job
			videoJob.source = source
			videoJob.destination = destination
			fullsizeVideoJobs <- videoJob
		}
	} else {
		fmt.Println("can't recognize file type for full-size copy", source)
	}
}

func thumbnailImageWorker(wg *sync.WaitGroup, thumbnailImageJobs chan job) {
	defer wg.Done()
	for job := range thumbnailImageJobs {
		resizeThumbnailImage(job.source, job.destination)
	}
}

func thumbnailVideoWorker(wg *sync.WaitGroup, thumbnailVideoJobs chan job) {
	defer wg.Done()
	for job := range thumbnailVideoJobs {
		resizeThumbnailVideo(job.source, job.destination)
	}
}

func thumbnailCopyFile(source string, destination string, thumbnailImageJobs chan job, thumbnailVideoJobs chan job) {
	if isImageFile(source) {
		destination = stripExtension(destination) + thumbnailExtension
		if optDryRun {
			fmt.Println("Would thumbnail copy image", source, "to", destination)
		} else {
			var imageJob job
			imageJob.source = source
			imageJob.destination = destination
			thumbnailImageJobs <- imageJob
		}
	} else if isVideoFile(source) {
		destination = stripExtension(destination) + thumbnailExtension
		if optDryRun {
			fmt.Println("Would thumbnail copy video", source, "to", destination)
		} else {
			var videoJob job
			videoJob.source = source
			videoJob.destination = destination
			thumbnailVideoJobs <- videoJob
		}
	} else {
		fmt.Println("can't recognize file type for thumbnail copy", source)
	}
}

func cleanGallery(gallery directory) {
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
		cleanGallery(dir)
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

// Check that source directory root doesn't contain a name reserved for our output directories
func checkReservedNames(inputDirectory string) {
	list, err := ioutil.ReadDir(inputDirectory)
	checkError(err)

	for _, entry := range list {
		if entry.Name() == optSymlinkDir || entry.Name() == optFullsizeDir || entry.Name() == optThumbnailDir {
			fmt.Fprintf(os.Stderr, "Source directory root cannot contain file or folder with\n")
			fmt.Fprintf(os.Stderr, "reserved names '%s', '%s' or '%s'\n", optSymlinkDir, optFullsizeDir, optThumbnailDir)
			os.Exit(1)
		}
	}
}

func main() {
	var inputDirectory string
	var outputDirectory string

	var gallery directory
	var source directory

	// parse command-line args and set HTML template ready
	outputDirectory, inputDirectory = parseArgs()

	fmt.Println(os.Args[0], ": Creating photo gallery")
	fmt.Println("")
	fmt.Println("Gathering photos and videos from:", inputDirectory)
	fmt.Println("Creating static gallery in:", outputDirectory)
	if optDryRun {
		fmt.Println("Only dry run, not actually changing anything")
	}
	fmt.Println("")

	// check that source directory doesn't have reserved directory or file names
	checkReservedNames(inputDirectory)

	// create directory structs by recursing through source and gallery directories
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

	changes := countChanges(source)
	if changes > 0 {
		var progressBar *pb.ProgressBar
		if !optDryRun {
			progressBar = pb.StartNew(changes)
			vips.LoggingSettings(nil, vips.LogLevelMessage)
			vips.Startup(nil)
			defer vips.Shutdown()
		}

		fullsizeImageJobs := make(chan job, 100000)
		thumbnailImageJobs := make(chan job, 100000)
		fullsizeVideoJobs := make(chan job, 100000)
		thumbnailVideoJobs := make(chan job, 100000)

		var wg sync.WaitGroup

		for i := 1; i <= imageWorkerPoolSize; i++ {
			wg.Add(2)
			go fullsizeImageWorker(&wg, fullsizeImageJobs, progressBar)
			go thumbnailImageWorker(&wg, thumbnailImageJobs)
		}

		if !optIgnoreVideos {
			for i := 1; i <= videoWorkerPoolSize; i++ {
				wg.Add(2)
				go fullsizeVideoWorker(&wg, fullsizeVideoJobs, progressBar)
				go thumbnailVideoWorker(&wg, thumbnailVideoJobs)
			}
		}

		// create the gallery
		createGallery(source, source.name, gallery, fullsizeImageJobs, thumbnailImageJobs, fullsizeVideoJobs, thumbnailVideoJobs)

		close(fullsizeImageJobs)
		close(fullsizeVideoJobs)
		close(thumbnailImageJobs)
		close(thumbnailVideoJobs)
		wg.Wait()

		if !optDryRun {
			progressBar.Finish()
		}
		fmt.Println("Gallery created!")
	} else {
		fmt.Println("No pictures to update!")
	}

	if optCleanUp {
		fmt.Println("\nCleaning up unused media files in output directory...")
		cleanGallery(gallery)
	}

	fmt.Println("\nDone!")
}
