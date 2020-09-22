package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"
)

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
	list, err := ioutil.ReadDir(directory)
	checkError(err)

	if len(list) == 0 {
		return true
	}
	return false
}

func isMediaFile(filename string) (isMedia bool) {
	//TODO fix add strings.toLower() once goimport stops removing it
	switch filepath.Ext(filename) {
	case ".jpg", ".jpeg", ".heic", ".png", ".gif", ".tif":
		return true
	case ".cr2", ".raw", ".arw":
		return true
	case ".mp4", ".mov", ".3gp", ".avi", ".mts", ".m4v", ".mpg":
		return true
	default:
		return false
	}
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
				root.subdirectories = append(root.subdirectories, recurseDirectory(filepath.Join(thisDirectory, entry.Name()), filepath.Join(relativeDirectory, entry.Name())))
			}
		} else {
			if isMediaFile(entry.Name()) {
				root.files = append(root.files, file{name: entry.Name(), modTime: entry.ModTime(), relPath: filepath.Join(relativeDirectory, entry.Name()), absPath: filepath.Join(thisDirectory, entry.Name()), exists: false})
			}
		}
	}

	return (root)
}

func compareDirectories(source *directory, gallery *directory, changes *int) {
	for i, inputFile := range source.files {
		for j, outputFile := range gallery.files {
			// TODO what if modtimes are exact same as expected
			if inputFile.name == outputFile.name && outputFile.modTime.After(inputFile.modTime) {
				source.files[i].exists = true
				gallery.files[j].exists = true
				*changes--
			}
		}
	}

	for k, inputDir := range source.subdirectories {
		for l, outputDir := range gallery.subdirectories {
			if inputDir.name == outputDir.name {
				compareDirectories(&(gallery.subdirectories[l]), &(source.subdirectories[k]), changes)
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

func createSymlinks(source directory, gallery directory, optDryRun bool) {
	fmt.Println(source)
	fmt.Println(gallery.absPath)
	fmt.Println(optDryRun)
}

func transformChanges(source directory, gallery directory, optDryRun bool) {
	// create symlinks in gallery for non-existing source files
	createSymlinks(source, gallery, optDryRun)

	// create full-size photos in gallery for non-existing source files

	// create thumbnail photos in gallery for non-existing source files

	// create index.html files

	// delete non-existing gallery symlinks, full-size photos and thumbnails

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
	compareDirectories(&source, &gallery, &changes)
	fmt.Println(changes, "new pictures to update")

	// create the gallery and delete stale pictures
	transformChanges(source, gallery, optDryRun)
}
