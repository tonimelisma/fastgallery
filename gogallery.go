package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"
)

func parseArgs() (inputDirectory string, outputDirectory string) {
	outputDirectoryPtr := flag.String("o", ".", "Output root directory for gallery")

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

	if isEmpty(flag.Args()[0]) {
		fmt.Fprintf(os.Stderr, "%s: Input directory is empty: %s\n", os.Args[0], flag.Args()[0])
		os.Exit(1)
	}

	return *outputDirectoryPtr, flag.Args()[0]
}

type file struct {
	name      string
	modTime   time.Time
	thumbnail string
}

type directory struct {
	name           string
	modTime        time.Time
	thumbnail      string
	subdirectories []directory
	files          []file
}

func checkError(e error) {
	if e != nil {
		panic(e)
	}
}

func isEmpty(directory string) (isEmpty bool) {
	list, err := ioutil.ReadDir(directory)
	checkError(err)

	if len(list) == 0 {
		return true
	}
	return false
}

func discoverAsIs(outputDirectory string) (asIs directory) {
	asIs.name = filepath.Base(outputDirectory)

	list, err := ioutil.ReadDir(outputDirectory)
	checkError(err)

	for _, entry := range list {
		if entry.IsDir() {
			asIs.subdirectories = append(asIs.subdirectories, directory{name: entry.Name(), modTime: entry.ModTime()})
		} else {
			asIs.files = append(asIs.files, file{name: entry.Name(), modTime: entry.ModTime()})
		}
	}

	return (asIs)
	//fmt.Println(root)
}

func main() {
	var inputDirectory string
	var outputDirectory string
	var asIs directory

	outputDirectory, inputDirectory = parseArgs()

	fmt.Println(os.Args[0], ": Creating photo gallery")
	fmt.Println("")
	fmt.Println("Gathering photos and videos from:", inputDirectory)
	fmt.Println("Creating static gallery in:", outputDirectory)
	fmt.Println("")

	asIs = discoverAsIs(outputDirectory)
	fmt.Println("As-is: ", asIs)
}
