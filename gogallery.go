package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/h2non/bimg"
)

// global defaults
var optSymlinkDir = "Original"
var optFullsizeDir = "Pictures"
var optThumbnailDir = "Thumbnails"

var optDirectoryMode os.FileMode = 0755
var optFileMode os.FileMode = 0644

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

func compareDirectories(source *directory, gallery *directory, changes *int) {
	for i, inputFile := range source.files {
		for j, outputFile := range gallery.files {
			if inputFile.name == outputFile.name {
				gallery.files[j].exists = true
				if !outputFile.modTime.Before(inputFile.modTime) {
					source.files[i].exists = true
					*changes--
				}
			}
		}
	}

	for k, inputDir := range source.subdirectories {
		for l, outputDir := range gallery.subdirectories {
			if inputDir.name == outputDir.name {
				compareDirectories(&(source.subdirectories[l]), &(gallery.subdirectories[k]), changes)
			}
		}
	}
}

func countFiles(source directory, inputChanges int) (outputChanges int) {
	outputChanges = inputChanges + len(source.files)

	for _, dir := range source.subdirectories {
		outputChanges = countFiles(dir, outputChanges)
	}

	return outputChanges
}

func createGallery(source directory, sourceRootDir string, gallery directory, optDryRun bool) {
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
		}
	}

	// Recurse into each subdirectory to continue creating symlinks
	for _, dir := range source.subdirectories {
		createGallery(dir, sourceRootDir, gallery, optDryRun)
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
		err := os.Symlink(source, destination)
		checkError(err)
	}
}

func thumbnailImage(source string, destination string) {
	buffer, err := bimg.Read(source)
	checkError(err)

	newImage, err := bimg.NewImage(buffer).ResizeAndCrop(1920, 1080)
	checkError(err)

	bimg.Write(destination, newImage)
}

func fullsizeImage(source string, destination string) {
	buffer, err := bimg.Read(source)
	checkError(err)

	newImage, err := bimg.NewImage(buffer).Thumbnail(150)
	checkError(err)

	bimg.Write(destination, newImage)
}

func fullsizeCopyFile(source string, destination string, optDryRun bool) {
	if isImageFile(source) {
		if optDryRun {
			fmt.Println("Would full-size copy image", source, "to", destination)
		} else {
			// TODO Image magic here
		}
	} else if isVideoFile(source) {
		if optDryRun {
			fmt.Println("Would full-size copy video ", source, "to", destination)
		} else {
			// TODO Image magic here
		}
	} else {
		fmt.Println("can't recognize file type for copy")
	}
}

func thumbnailCopyFile(source string, destination string, optDryRun bool) {
	if isImageFile(source) {
		if optDryRun {
			fmt.Println("Would thumbnail copy image", source, "to", destination)
		} else {
			// TODO Video magic here
		}
	} else if isVideoFile(source) {
		if optDryRun {
			fmt.Println("Would thumbnail copy video ", source, "to", destination)
		} else {
			// TODO Video magic here
		}
	} else {
		fmt.Println("can't recognize file type for copy")
	}
}

func cleanGallery(gallery directory, optDryRun bool) {
	for _, file := range gallery.files {
		if !file.exists {
			if optDryRun {
				fmt.Println("Would delete", file.absPath)
				fmt.Println(file)
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
	var changes int

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
	changes = countFiles(source, 0)

	// check whether gallery already has up-to-date pictures of sources,
	// mark existing pictures in structs
	fmt.Println(changes, "new pictures to update")
	for _, dir := range gallery.subdirectories {
		if dir.name == optSymlinkDir {
			compareDirectories(&source, &dir, &changes)
		}
	}
	for _, dir := range gallery.subdirectories {
		if dir.name == optFullsizeDir {
			compareDirectories(&source, &dir, &changes)
		}
	}
	for _, dir := range gallery.subdirectories {
		if dir.name == optThumbnailDir {
			compareDirectories(&source, &dir, &changes)
		}
	}
	fmt.Println(changes, "new pictures to update")
	//fmt.Println(source)
	fmt.Println("")
	fmt.Println(gallery)
	fmt.Println("")

	// create the gallery
	createGallery(source, source.name, gallery, optDryRun)

	// delete stale pictures
	cleanGallery(gallery, optDryRun)
}
