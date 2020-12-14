package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/cheggaaa/pb/v3"
	"github.com/davidbyttow/govips/v2/vips"

	_ "net/http/pprof"
)

// assets
const assetDirectory = "/usr/local/share/fastgallery"
const assetPlaybuttonImage = "playbutton.png"
const assetFolderImage = "folder.png"
const assetBackImage = "back.png"
const assetHTMLTemplate = "gallery.gohtml"

var assetCSS = []string{"fastgallery.css", "primer.css"}
var assetJS = []string{"fastgallery.js", "feather.min.js"}

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
var optVerbose = false
var optCleanUp = false
var optMemoryUse = false

// this function parses command-line arguments
func parseArgs() (inputDirectory string, outputDirectory string) {
	outputDirectoryPtr := flag.String("o", ".", "Output root directory for gallery")
	optIgnoreVideosPtr := flag.Bool("i", false, "Ignore video files")
	optCleanUpPtr := flag.Bool("c", false, "Clean up - delete stale media files from output directory")
	optDryRunPtr := flag.Bool("d", false, "Dry run - don't make changes, only explain what would be done")
	optVerbosePtr := flag.Bool("v", false, "Verbose - print debugging information to stderr")
	optProfile := flag.Bool("p", false, "Run Go pprof profiling service for debugging")
	optMemory := flag.Bool("m", false, "Minimize memory usage")

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

	if isEmptyDir(flag.Args()[0]) {
		fmt.Fprintf(os.Stderr, "%s: Input directory is empty: %s\n", os.Args[0], flag.Args()[0])
		os.Exit(1)
	}

	if *optMemory {
		optMemoryUse = true
	}

	if *optProfile {
		go func() {
			log.Println(http.ListenAndServe("localhost:6060", nil))
		}()
	}

	if *optDryRunPtr {
		optDryRun = true
	}

	if *optCleanUpPtr {
		optCleanUp = true
	}

	if *optVerbosePtr {
		optVerbose = true
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

	inputDirectory, err := filepath.Abs(flag.Args()[0])
	checkError(err)
	if err != nil {
		os.Exit(1)
	}
	outputDirectory, err = filepath.Abs(*outputDirectoryPtr)
	checkError(err)
	if err != nil {
		os.Exit(1)
	}

	return
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
	Subdirectories []string
	Files          []struct {
		Filename  string
		Thumbnail string
		Fullsize  string
		Original  string
	}
	CSS        []string
	JS         []string
	FolderIcon string
	BackIcon   string
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
	if err != nil {
		return
	}

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

// Checks if given directory entry is symbolic link to a directory
func isSymlinkDir(directory string, entry os.FileInfo) (is bool) {
	if entry.Mode()&os.ModeSymlink != 0 {
		realPath, err := filepath.EvalSymlinks(filepath.Join(directory, entry.Name()))
		checkError(err)

		fileinfo, err := os.Lstat(realPath)
		checkError(err)

		if fileinfo.IsDir() {
			return true
		}
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
	if err != nil {
		return
	}

	for _, entry := range list {
		if entry.IsDir() || isSymlinkDir(thisDirectory, entry) {
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
				compareDirectories(&(source.subdirectories[k]), &(gallery.subdirectories[l]))
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

	if len(source.subdirectories) > 0 || len(source.files) > 0 {
		createHTML(source.subdirectories, source.files, sourceRootDir, htmlDirectoryPath)
	}
}

func copy(sourceDir string, destDir string, filename string) {
	sourceFilename := filepath.Join(sourceDir, filename)
	destFilename := filepath.Join(destDir, filename)

	_, err := os.Stat(sourceFilename)
	checkError(err)
	if err != nil {
		return
	}

	sourceHandle, err := os.Open(sourceFilename)
	checkError(err)
	if err != nil {
		return
	}
	defer sourceHandle.Close()

	destHandle, err := os.Create(destFilename)
	checkError(err)
	if err != nil {
		return
	}
	defer destHandle.Close()

	_, err = io.Copy(destHandle, sourceHandle)
	checkError(err)
	if err != nil {
		return
	}
}

func copyRootAssets(gallery directory) {
	for _, file := range assetCSS {
		copy(assetDirectory, gallery.absPath, file)
	}
	for _, file := range assetJS {
		copy(assetDirectory, gallery.absPath, file)
	}
	copy(assetDirectory, gallery.absPath, assetFolderImage)
	copy(assetDirectory, gallery.absPath, assetBackImage)
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

// getHTMLRootPathRelative gets the relative ../../ needed to get to the gallery root path
// filename provided is a given file in the directory we're in
// we strip the filename and source gallery root directory to get the
// number of subdirectories we've descended. Used to link to CSS/JS assets in gallery root.
func getHTMLRootPathRelative(filename string) (pathRelative string) {
	directoryList := strings.Split(filename, "/")
	pathRelative = ""
	for i := 2; i < len(directoryList); i = i + 1 {
		pathRelative = pathRelative + "../"
	}
	return pathRelative
}

func createHTML(subdirectories []directory, files []file, sourceRootDir string, htmlDirectoryPath string) {
	htmlFilePath := filepath.Join(htmlDirectoryPath, "index.html")

	var rootEscape string
	if len(files) > 0 {
		rootEscape = getHTMLRootPathRelative(files[0].relPath)
	} else {
		rootEscape = getHTMLRootPathRelative(subdirectories[0].relPath)
	}
	var data htmlData

	for _, file := range assetCSS {
		data.CSS = append(data.CSS, rootEscape+file)
	}
	for _, file := range assetJS {
		data.JS = append(data.JS, rootEscape+file)
	}
	data.FolderIcon = rootEscape + assetFolderImage
	if len(rootEscape) > 0 {
		data.BackIcon = rootEscape + assetBackImage
	}

	data.Title = filepath.Base(htmlDirectoryPath)
	for _, dir := range subdirectories {
		data.Subdirectories = append(data.Subdirectories, dir.name)
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
		cookedTemplate, err := template.ParseFiles(filepath.Join(assetDirectory, assetHTMLTemplate))
		checkError(err)
		if err != nil {
			return
		}

		htmlFileHandle, err := os.Create(htmlFilePath)
		checkError(err)
		if err != nil {
			return
		}
		defer htmlFileHandle.Close()

		err = cookedTemplate.Execute(htmlFileHandle, data)
		checkError(err)
		if err != nil {
			return
		}

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
			if err != nil {
				return
			}
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
			if err != nil {
				return
			}
		}
		err := os.Symlink(source, destination)
		checkError(err)
		if err != nil {
			return
		}
	}
}

func resizeThumbnailVideo(source string, destination string) {
	ffmpegCommand := exec.Command("ffmpeg", "-y", "-i", source, "-ss", "00:00:00", "-vframes", "1", "-vf", fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d", thumbnailWidth, thumbnailHeight, thumbnailWidth, thumbnailHeight), "-loglevel", "fatal", destination)
	ffmpegCommand.Stdout = os.Stdout
	ffmpegCommand.Stderr = os.Stderr

	err := ffmpegCommand.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not create thumbnail of video %s", source)
	}

	// Take thumbnail and overlay triangle image on top of it

	image, err := vips.NewImageFromFile(destination)
	checkError(err)
	if err != nil {
		return
	}

	// TODO preload overlay globally to reduce overhead
	playbuttonOverlayImage, err := vips.NewImageFromFile(filepath.Join(assetDirectory, assetPlaybuttonImage))
	checkError(err)
	if err != nil {
		return
	}

	// overlay play button in the middle of thumbnail picture
	err = image.Composite(playbuttonOverlayImage, vips.BlendModeOver, (thumbnailWidth/2)-(playbuttonOverlayImage.Width()/2), (thumbnailHeight/2)-(playbuttonOverlayImage.Height()/2))
	checkError(err)
	if err != nil {
		return
	}

	ep := vips.NewDefaultJPEGExportParams()
	imageBytes, _, err := image.Export(ep)
	checkError(err)
	if err != nil {
		return
	}

	err = ioutil.WriteFile(destination, imageBytes, optFileMode)
	checkError(err)
	if err != nil {
		return
	}
}

func resizeFullsizeVideo(source string, destination string) {
	ffmpegCommand := exec.Command("ffmpeg", "-y", "-i", source, "-pix_fmt", "yuv420p", "-vcodec", "libx264", "-acodec", "aac", "-movflags", "faststart", "-r", "24", "-vf", "scale='min(640,iw)':'min(640,ih)':force_original_aspect_ratio=decrease", "-crf", "28", "-loglevel", "fatal", destination)
	ffmpegCommand.Stdout = os.Stdout
	ffmpegCommand.Stderr = os.Stderr

	err := ffmpegCommand.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not create full-size video of %s", source)
	}
}

func resizeThumbnailImage(source string, destination string) {
	if thumbnailExtension == ".jpg" {
		image, err := vips.NewImageFromFile(source)
		checkError(err)
		if err != nil {
			return
		}

		// TODO fix inefficiency, autorotate before resizing
		// Needed for now to simplify thumbnailing calculations
		err = image.AutoRotate()
		checkError(err)
		if err != nil {
			return
		}

		// Resize and crop picture to suitable
		ratio := float64(image.Height()) / float64(image.Width())
		targetRatio := float64(thumbnailHeight) / float64(thumbnailWidth)

		if ratio < targetRatio {
			// Picture is wider than thumbnail
			// Resize by height to fit thumbnail size, then crop left and right edge
			scale := float64(thumbnailHeight) / float64(image.Height())
			err = image.Resize(scale, vips.KernelAuto)
			checkError(err)
			if err != nil {
				return
			}

			// Calculate how much to crop from each edge
			cropAmount := (image.Width() - thumbnailWidth) / 2
			err = image.ExtractArea(cropAmount, 0, thumbnailWidth, thumbnailHeight)
			checkError(err)
			if err != nil {
				return
			}
		} else if ratio > targetRatio {
			// Picture is higher than thumbnail
			// Resize by width to fit thumbnail size, then crop top and bottom edge
			scale := float64(thumbnailWidth) / float64(image.Width())
			err = image.Resize(scale, vips.KernelAuto)
			checkError(err)
			if err != nil {
				return
			}

			// Calculate how much to crop from each edge
			cropAmount := (image.Height() - thumbnailHeight) / 2
			err = image.ExtractArea(0, cropAmount, thumbnailWidth, thumbnailHeight)
			checkError(err)
			if err != nil {
				return
			}
		} else {
			// Picture has same aspect ratio as thumbnail
			// Resize, but no need to crop after resize
			scale := float64(thumbnailWidth) / float64(image.Width())
			err = image.Resize(scale, vips.KernelAuto)
			checkError(err)
			if err != nil {
				return
			}
		}

		ep := vips.NewDefaultJPEGExportParams()
		imageBytes, _, err := image.Export(ep)
		checkError(err)
		if err != nil {
			return
		}

		err = ioutil.WriteFile(destination, imageBytes, optFileMode)
		checkError(err)
		if err != nil {
			return
		}
	} else {
		fmt.Fprintf(os.Stderr, "Can't figure out what format to convert thumbnail image to: %s\n", destination)
	}
}

func resizeFullsizeImage(source string, destination string) {
	if fullsizePictureExtension == ".jpg" {
		image, err := vips.NewImageFromFile(source)
		checkError(err)
		if err != nil {
			return
		}

		err = image.AutoRotate()
		checkError(err)
		if err != nil {
			return
		}

		scale := float64(fullsizeMaxWidth) / float64(image.Width())
		if (scale * float64(image.Height())) > float64(fullsizeMaxHeight) {
			scale = float64(fullsizeMaxHeight) / float64(image.Height())
		}

		err = image.Resize(scale, vips.KernelAuto)
		checkError(err)
		if err != nil {
			return
		}

		ep := vips.NewDefaultJPEGExportParams()
		imageBytes, _, err := image.Export(ep)
		checkError(err)
		if err != nil {
			return
		}

		err = ioutil.WriteFile(destination, imageBytes, optFileMode)
		checkError(err)
		if err != nil {
			return
		}
	} else {
		fmt.Fprintf(os.Stderr, "Can't figure out what format to convert full size image to: %s\n", destination)
	}
}

func fullsizeImageWorker(wg *sync.WaitGroup, imageJobs chan job, progressBar *pb.ProgressBar) {
	defer wg.Done()
	for job := range imageJobs {
		if optVerbose {
			fmt.Fprintf(os.Stderr, "Creating full size image of %s\n", job.source)
		}
		resizeFullsizeImage(job.source, job.destination)
		if !optDryRun {
			progressBar.Increment()
			if optMemoryUse {
				runtime.GC()
			}
		}
	}
}

func fullsizeVideoWorker(wg *sync.WaitGroup, videoJobs chan job, progressBar *pb.ProgressBar) {
	defer wg.Done()
	for job := range videoJobs {
		if optVerbose {
			fmt.Fprintf(os.Stderr, "Creating full size video of %s\n", job.source)
		}
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
		if optVerbose {
			fmt.Fprintf(os.Stderr, "Creating thumbnail image of %s\n", job.source)
		}
		resizeThumbnailImage(job.source, job.destination)
	}
}

func thumbnailVideoWorker(wg *sync.WaitGroup, thumbnailVideoJobs chan job) {
	defer wg.Done()
	for job := range thumbnailVideoJobs {
		if optVerbose {
			fmt.Fprintf(os.Stderr, "Creating thumbnail video of %s\n", job.source)
		}
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
				if err != nil {
					return
				}
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
			if err != nil {
				return
			}
		}
	}
}

// Check that source directory root doesn't contain a name reserved for our output directories
func checkReservedNames(inputDirectory string) {
	list, err := ioutil.ReadDir(inputDirectory)
	checkError(err)
	if err != nil {
		return
	}

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
	inputDirectory, outputDirectory = parseArgs()

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
	if _, err := os.Stat(outputDirectory); !os.IsNotExist(err) {
		gallery = recurseDirectory(outputDirectory, "")
	} else {
		gallery.name = filepath.Base(outputDirectory)
		gallery.absPath, _ = filepath.Abs(outputDirectory)
		gallery.relPath = ""
	}
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
		if optCleanUp {
			fmt.Print("Cleaning up unused media files in output directory...")
			if _, err := os.Stat(gallery.absPath); !os.IsNotExist(err) {
				cleanGallery(gallery)
				fmt.Println("done.")
			} else {
				fmt.Println("already empty.")
			}
		}

		// create gallery directory if it doesn't exist
		createDirectory(gallery.absPath)

		var progressBar *pb.ProgressBar
		if !optDryRun {
			fmt.Println("Creating gallery...")
			progressBar = pb.StartNew(changes)
			if optVerbose {
				vips.LoggingSettings(nil, vips.LogLevelDebug)
				vips.Startup(&vips.Config{
					CacheTrace:   false,
					CollectStats: false,
					ReportLeaks:  true})
			} else {
				vips.LoggingSettings(nil, vips.LogLevelError)
				vips.Startup(nil)
			}
			defer vips.Shutdown()
		}

		fullsizeImageJobs := make(chan job, 10000)
		thumbnailImageJobs := make(chan job, 10000)
		fullsizeVideoJobs := make(chan job, 10000)
		thumbnailVideoJobs := make(chan job, 10000)

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

		if !optDryRun {
			copyRootAssets(gallery)
		}

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

	fmt.Println("\nDone!")
}
