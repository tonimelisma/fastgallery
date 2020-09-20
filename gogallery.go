package main

import (
	"flag"
	"fmt"
)

func parseArgs() (outputDirectory string, inputDirectories []string) {
	outputDirectoryPtr := flag.String("o", ".", "Output directory")

	flag.Parse()

	fmt.Println("o:", *outputDirectoryPtr)
	fmt.Println("tail:")
	for i, arg := range flag.Args() {
		fmt.Println("item", i, "is", arg)
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
