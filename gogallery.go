package main

import (
	"flag"
	"fmt"
	"os"
)

func parseArgs() (outputDirectory string, inputDirectories []string) {
	outputDirectoryPtr := flag.String("o", ".", "Output root directory for gallery")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [OPTION]... DIRECTORY...\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Create a static photo and video gallery from directories.\n")
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "Will recurse each of supplied DIRECTORY \n")
		fmt.Fprintf(os.Stderr, "\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	if flag.NArg() == 0 {
		fmt.Fprintf(os.Stderr, "%s: missing directories to use as input\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Try '%s -h' for more information.\n", os.Args[0])
		os.Exit(1)
	}

	for _, arg := range flag.Args() {
		inputDirectories = append(inputDirectories, arg)
	}

	return *outputDirectoryPtr, inputDirectories
}

func main() {
	var outputDirectory string
	var inputDirectories []string

	outputDirectory, inputDirectories = parseArgs()

	for dir := range inputDirectories {
		fmt.Println("Gathering photos and videos from:")
		fmt.Println(dir)
	}
	fmt.Println("Creating static gallery in:", outputDirectory)
}
