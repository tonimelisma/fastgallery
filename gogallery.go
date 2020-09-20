package main

import (
	"flag"
	"fmt"
)

func parseArgs() (outputDirectory string, inputDirectories []string) {
	outputDirectoryPtr := flag.String("o", ".", "Output directory")

	flag.Parse()

	fmt.Println("o:", *outputDirectoryPtr)
	if flag.NArg() == 0 {
		fmt.Println("missing argument")
	}
	fmt.Println("tail:")
	for _, arg := range flag.Args() {
		inputDirectories = append(inputDirectories, arg)
	}

	outputDirectory = "out"
	return outputDirectory, inputDirectories
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
