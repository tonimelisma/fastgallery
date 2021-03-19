package main

import (
	"embed"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"text/template"
	"time"

	"github.com/cheggaaa/pb/v3"
	"github.com/davidbyttow/govips/v2/vips"

	"github.com/alexflint/go-arg"
)

// Embed all static assets
//go:embed assets
var assets embed.FS

// Define global exit function, so unit tests can override this
var exit = os.Exit

// Define global state for slice of WIP transformation jobs, used by signalHandler()
var wipJobs = make(map[string]transformationJob)
var wipJobMutex = sync.Mutex{}

// configuration state is stored in this struct
type configuration struct {
	files struct {
		originalDir    string
		fullsizeDir    string
		thumbnailDir   string
		directoryMode  os.FileMode
		fileMode       os.FileMode
		imageExtension string
		videoExtension string
	}
	assets struct {
		assetsDir        string
		htmlFile         string
		backIcon         string
		folderIcon       string
		playIcon         string
		htmlTemplate     string
		manifestFile     string
		manifestTemplate string
	}
	media struct {
		thumbnailWidth    int
		thumbnailHeight   int
		fullsizeMaxWidth  int
		fullsizeMaxHeight int
		videoMaxSize      int
	}
	concurrency int
}

// initialize the configuration with hardcoded defaults
func initializeConfig() (config configuration) {
	config.files.originalDir = "_original"
	config.files.fullsizeDir = "_fullsize"
	config.files.thumbnailDir = "_thumbnail"
	config.files.directoryMode = 0755
	config.files.fileMode = 0644
	config.files.imageExtension = ".jpg"
	config.files.videoExtension = ".mp4"

	config.assets.assetsDir = "assets"
	config.assets.htmlFile = "index.html"
	config.assets.htmlTemplate = "gallery.gohtml"
	config.assets.backIcon = "back.png"
	config.assets.folderIcon = "folder.png"
	config.assets.playIcon = "playbutton.png"
	config.assets.manifestFile = "manifest.json"
	config.assets.manifestTemplate = "manifest.json.tmpl"

	config.media.thumbnailWidth = 280
	config.media.thumbnailHeight = 210
	config.media.fullsizeMaxWidth = 1920
	config.media.fullsizeMaxHeight = 1080
	config.media.videoMaxSize = 640

	// TODO adjust based on cores
	config.concurrency = 4

	return config
}

// file struct represents an individual media file
// relPath is the relative path to from source/gallery root directory.
// For source files, exists marks whether it exists in the gallery and doesn't need to be copied.
// In this case, gallery has all three transformed files (original, full-size and thumbnail) and
// the thumbnail's modification date isn't before the original source file's.
// For gallery files, exists marks whether all three gallery files are in place (original, full-size
// and thumbnail) and there's a corresponding source file.
type file struct {
	name    string
	relPath string
	absPath string
	modTime time.Time
	exists  bool
}

// directory struct is one directory, which contains files and subdirectories
// relPath is the relative path from source/gallery root directory
// For source directories, exists reflects whether the directory exists in the gallery
// For gallery directories, exists reflects whether there's a corresponding source directory
type directory struct {
	name           string
	relPath        string
	absPath        string
	modTime        time.Time
	files          []file
	subdirectories []directory
	exists         bool
}

// htmlData struct is loaded with all the information required to generate the html from template
// TODO refactor structure inside only function where its used
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

// transformationJob struct is used to communicate needed image/video transformations to
// individual concurrent goroutines
type transformationJob struct {
	filename          string
	sourceFilepath    string
	thumbnailFilepath string
	fullsizeFilepath  string
	originalFilepath  string
}

// exists checks whether given file, directory or symlink exists
func exists(filepath string) bool {
	if _, err := os.Stat(filepath); os.IsNotExist(err) {
		return false
	}
	return true
}

// isDirectory checks whether provided path is a directory or symlink to one
// resolves symlinks only one level deep
func isDirectory(directory string) bool {
	filestat, err := os.Stat(directory)
	if os.IsNotExist(err) {
		return false
	}

	if filestat.IsDir() {
		return true
	}

	if filestat.Mode()&os.ModeSymlink != 0 {
		realDirectory, err := filepath.EvalSymlinks(directory)
		if err != nil {
			log.Printf("error: %s\n", err.Error())
			return false
		}

		realFilestat, err := os.Stat(realDirectory)
		if err != nil {
			log.Printf("error: %s\n", err.Error())
			return false
		}

		if realFilestat.IsDir() {
			return true
		}
	}

	return false
}

// Validate that source and gallery directories given as parameters
// are valid directories. Return absolue path of source and gallery
func validateSourceAndGallery(source string, gallery string) (string, string) {
	var err error

	source, err = filepath.Abs(source)
	if err != nil {
		log.Println("error:", err.Error())
		exit(1)
	}

	if !isDirectory(source) {
		log.Println("Source directory doesn't exist:", source)
		exit(1)
	}

	gallery, err = filepath.Abs(gallery)
	if err != nil {
		log.Println("error:", err.Error())
		exit(1)
	}

	if !isDirectory(gallery) {
		// Ok, gallery isn't a directory but check whether the parent directory is
		// and we're supposed to create gallery there during runtime
		galleryParent, err := filepath.Abs(filepath.Join(gallery, "/../"))
		if err != nil {
			log.Println("error:", err.Error())
			exit(1)
		}

		if !isDirectory(galleryParent) {
			log.Println("Neither gallery directory or it's parent directory exist:", gallery)
			exit(1)
		}
	}

	return source, gallery
}

// Checks whether directory has media files, or subdirectories with media files.
// If there's a subdirectory that's empty or that has directories or files which
// aren't media files, we leave that out of the directory tree.
func dirHasMediafiles(directory string, noVideos bool) (isEmpty bool) {
	list, err := os.ReadDir(directory)
	if err != nil {
		// If we can't read the directory contents, it doesn't have media files in it
		return false
	}

	if len(list) == 0 {
		// If it's empty, it doesn't have media files
		return false
	}

	for _, entry := range list {
		entryAbsPath := filepath.Join(directory, entry.Name())
		if entry.IsDir() {
			// Recursion to subdirectories
			if dirHasMediafiles(entryAbsPath, noVideos) {
				return true
			}
		} else if isMediaFile(entryAbsPath, noVideos) {
			// We found at least one media file, return true
			return true
		}
	}

	// Didn't find at least one media file
	return false
}

// Check whether given path is a video file
func isVideoFile(filename string) bool {
	switch filepath.Ext(strings.ToLower(filename)) {
	case ".mp4", ".mov", ".3gp", ".avi", ".mts", ".m4v", ".mpg":
		return true
	default:
		return false
	}
}

// Check whether given path is an image file
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

// Check whether given absolute path is a media file
func isMediaFile(filename string, noVideos bool) bool {
	if isImageFile(filename) {
		return true
	}

	if !noVideos && isVideoFile(filename) {
		return true
	}

	return false
}

// isSymlinkDir checks if given directory entry is symbolic link to a directory
func isSymlinkDir(targetPath string) (is bool) {
	entry, err := os.Lstat(targetPath)
	if err != nil {
		log.Println("Couldn't lstat dir path:", targetPath, err.Error())
		exit(1)
	}

	if entry.Mode()&os.ModeSymlink != 0 {
		realPath, err := filepath.EvalSymlinks(targetPath)
		if err != nil {
			return false
		}

		realEntry, err := os.Lstat(realPath)
		if err != nil {
			log.Println("Couldn't lstat file path:", targetPath)
			exit(1)
		}

		if realEntry.IsDir() {
			return true
		}
	}
	return false
}

// Create a recursive directory struct by traversing the directory absoluteDirectory.
// The function calls itself recursively, carrying state in the relativeDirectory parameter.
func createDirectoryTree(absoluteDirectory string, parentDirectory string, noVideos bool) (tree directory) {
	// In case the target directory doesn't exist, it's the gallery directory
	// which hasn't been created yet. We'll just create a dummy tree and return it.
	if !exists(absoluteDirectory) && parentDirectory == "" {
		tree.name = filepath.Base(absoluteDirectory)
		tree.relPath = parentDirectory
		tree.absPath, _ = filepath.Abs(absoluteDirectory)
		return
	}

	// Fill in the directory name and other basic info
	tree.name = filepath.Base(absoluteDirectory)
	tree.absPath, _ = filepath.Abs(absoluteDirectory)
	tree.relPath = parentDirectory
	absoluteDirectoryStat, _ := os.Stat(absoluteDirectory)
	tree.modTime = absoluteDirectoryStat.ModTime()

	// List directory contents
	list, err := os.ReadDir(absoluteDirectory)
	if err != nil {
		log.Println("Couldn't read directory contents:", absoluteDirectory)
		exit(1)
	}

	// If it's a directory and it has media files somewhere, add it to directories
	// If it's a media file, add it to the files
	for _, entry := range list {
		entryAbsPath := filepath.Join(absoluteDirectory, entry.Name())
		entryRelPath := filepath.Join(parentDirectory, entry.Name())
		if entry.IsDir() || isSymlinkDir(entryAbsPath) {
			if dirHasMediafiles(entryAbsPath, noVideos) {
				entrySubTree := createDirectoryTree(entryAbsPath, entryRelPath, noVideos)
				tree.subdirectories = append(tree.subdirectories, entrySubTree)
			}
		} else if isMediaFile(entryAbsPath, noVideos) {
			entryFileInfo, err := entry.Info()
			if err != nil {
				log.Println("Couldn't stat file information for media file:", entry.Name())
				exit(1)
			}
			entryFile := file{
				name:    entry.Name(),
				relPath: entryRelPath,
				absPath: entryAbsPath,
				modTime: entryFileInfo.ModTime(),
				exists:  false,
			}
			tree.files = append(tree.files, entryFile)
		}
	}
	return
}

// stripExtension strips the filename extension and returns the basename
func stripExtension(filename string) string {
	extension := filepath.Ext(filename)
	return filename[0 : len(filename)-len(extension)]
}

// reservedDirectory takes a path and checks whether it's a reserved name,
// i.e. one of the internal directories used by fastgallery
func reservedDirectory(path string, config configuration) bool {
	if path == config.files.thumbnailDir {
		return true
	}

	if path == config.files.fullsizeDir {
		return true
	}

	if path == config.files.originalDir {
		return true
	}

	return false
}

// reservedFile takes a path and checks whether it's a reserved file,
// such as one of our asset files
func reservedFile(path string, config configuration) bool {
	if path == config.assets.backIcon {
		return true
	}

	if path == config.assets.folderIcon {
		return true
	}

	if path == config.assets.manifestFile {
		return true
	}

	return false
}

// hasDirectoryChanged checks whether the gallery directory has changed and thus
// the HTML file needs to be updated. Could be due to:
// At least one non-existent source file or directory (will be created in gallery)
// We're doing a cleanup, and at least one non-existent gallery file or directory (will be removed from gallery)
func hasDirectoryChanged(source directory, gallery directory, cleanUp bool, config configuration) bool {
	for _, sourceFile := range source.files {
		if !sourceFile.exists {
			return true
		}
	}

	for _, sourceDir := range source.subdirectories {
		if !sourceDir.exists {
			return true
		}
	}

	// TODO recurse gallery simultaneously with source, nil if not available
	if cleanUp {
		for _, galleryFile := range gallery.files {
			if !reservedFile(galleryFile.name, config) && !galleryFile.exists {
				return true
			}
		}

		for _, galleryDir := range gallery.subdirectories {
			if !galleryDir.exists {
				return true
			}
		}
	}

	htmlPath := filepath.Join(gallery.absPath, source.relPath, config.assets.htmlFile)
	if _, err := os.Stat(htmlPath); os.IsNotExist(err) {
		return true
	}

	return false
}

// compareDirectoryTrees compares two directory trees (source and gallery) and marks
// each file that exists in both
func compareDirectoryTrees(source *directory, gallery *directory, config configuration) {
	// If we are comparing two directories, we know they both exist so we can set the
	// directory struct exists boolean
	source.exists = true
	gallery.exists = true

	// TODO fix bug where two source files with different extensions clash

	// Iterate over each file in source directory to see whether it exists in gallery
	for i, sourceFile := range source.files {
		sourceFileBasename := stripExtension(sourceFile.name)
		var thumbnailFile, fullsizeFile, originalFile *file

		// Go through all subdirectories, and check the ones that match
		// the thumbnail, full-size or original subdirectories.
		// Simultaneously, mark any gallery files which exist in source,
		// so any clean-up doesn't inadvertently delete them.
		for h, subDir := range gallery.subdirectories {
			if subDir.name == config.files.thumbnailDir {
				for i, outputFile := range gallery.subdirectories[h].files {
					outputFileBasename := stripExtension(outputFile.name)
					if sourceFileBasename == outputFileBasename {
						thumbnailFile = &gallery.subdirectories[h].files[i]
						thumbnailFile.exists = true
					}
				}
			} else if subDir.name == config.files.fullsizeDir {
				for j, outputFile := range gallery.subdirectories[h].files {
					outputFileBasename := stripExtension(outputFile.name)
					if sourceFileBasename == outputFileBasename {
						fullsizeFile = &gallery.subdirectories[h].files[j]
						fullsizeFile.exists = true
					}
				}
			} else if subDir.name == config.files.originalDir {
				for k, outputFile := range gallery.subdirectories[h].files {
					outputFileBasename := stripExtension(outputFile.name)
					if sourceFileBasename == outputFileBasename {
						originalFile = &gallery.subdirectories[h].files[k]
						originalFile.exists = true
					}
				}
			}
		}

		// If all of thumbnail, full-size and original files exist in gallery, and they're
		// modified after the source file, the source file exists and is up to date.
		// Otherwise we overwrite gallery files in case source file's been updated since the thumbnail
		// was created.
		if thumbnailFile != nil && fullsizeFile != nil && originalFile != nil {
			if thumbnailFile.modTime.After(sourceFile.modTime) {
				source.files[i].exists = true
			}
		}
	}

	// After checking all the files in this directory, recurse into each subdirectory and do the same
	for k, inputDir := range source.subdirectories {
		if !reservedDirectory(inputDir.name, config) {
			for l, outputDir := range gallery.subdirectories {
				if inputDir.name == outputDir.name {
					compareDirectoryTrees(&(source.subdirectories[k]), &(gallery.subdirectories[l]), config)
				}
			}
		}
	}
}

func countChanges(source directory, config configuration) (outputChanges int) {
	outputChanges = 0
	for _, file := range source.files {
		if !file.exists && !reservedFile(file.name, config) {
			outputChanges++
		}
	}

	for _, dir := range source.subdirectories {
		outputChanges = outputChanges + countChanges(dir, config)
	}

	return outputChanges
}

func findMissingHTMLFiles(gallery directory, config configuration) bool {
	htmlPath := filepath.Join(gallery.absPath, config.assets.htmlFile)
	if _, err := os.Stat(htmlPath); os.IsNotExist(err) {
		return true
	}

	for _, dir := range gallery.subdirectories {
		if !reservedDirectory(dir.name, config) {
			if findMissingHTMLFiles(dir, config) {
				return true
			}
		}
	}

	return false
}

func createDirectory(destination string, dryRun bool, dirMode os.FileMode) {
	if _, err := os.Stat(destination); os.IsNotExist(err) {
		if dryRun {
			log.Println("Would create directory:", destination)
		} else {
			err := os.Mkdir(destination, dirMode)
			if err != nil {
				log.Println("couldn't create directory", destination, err.Error())
				exit(1)
			}

			log.Println("Created directory:", destination)
		}
	}
}

func symlinkFile(source string, destination string) error {
	if _, err := os.Stat(destination); err == nil {
		err := os.Remove(destination)
		if err != nil {
			log.Println("couldn't remove symlink:", source, destination)
			return err
		}
	}
	err := os.Symlink(source, destination)
	if err != nil {
		log.Println("couldn't symlink:", source, destination)
		return err
	}

	return nil
}

// TODO add copyFile and option to use in lieu of symlinking
/*
func copyFile(source string, destination string) {
	_, err := os.Stat(sourceFilename)
	if err != nil {
		log.Println("couldn't copy source file:", sourceFilename, err.Error())
		exit(1)
	}

	sourceHandle, err := os.Open(sourceFilename)
	if err != nil {
		log.Println("couldn't open source file for copy:", sourceFilename, err.Error())
		exit(1)
	}
	defer sourceHandle.Close()

	destHandle, err := os.Create(destFilename)
	if err != nil {
		log.Println("couldn't create dest file:", destFilename, err.Error())
		exit(1)
	}
	defer destHandle.Close()

	_, err = io.Copy(destHandle, sourceHandle)
	if err != nil {
		log.Println("couldn't copy file:", sourceFilename, destFilename, err.Error())
		exit(1)
	}
}
*/

// getIconSize returns a square size (e.g. 48x48) of an icon based on its filename
// Icon filename must have a substring starting with a string of numbers followed by a consequential
// letter x and a string of more numbers
func getIconSize(iconPath string) (size string, err error) {
	iconPath = path.Base(iconPath)

	re := regexp.MustCompile(`[0-9]+x[0-9]+`)
	size = re.FindString(iconPath)

	if size == "" {
		err = errors.New("size not found in path: " + iconPath)
		return size, err
	}

	return size, nil
}

// getIconType returns icon file format type (e.g. image/png) of an icon based on its filename
func getIconType(iconPath string) (filetype string, err error) {
	iconPath = path.Base(iconPath)

	switch filepath.Ext(iconPath) {
	case ".png":
		return "image/png", nil
	}

	err = errors.New("could not decide icon filetype: " + iconPath)
	return "", err
}

// createPWAManifest creates a customized manifest.json for a PWA if PWA url is supplied in args
func createPWAManifest(gallery directory, source directory, dryRun bool, config configuration) {
	// TODO Fill in data structure, load template and execute it
	// TODO Iterate over icons, grab size from filename
	// TODO Add manifest link to HTMLs
	// TODO Add apple-touch-icon to HTML
	// TODO register service worker in HTML, add manifest and apple-touch-icon links to head

	var PWAData = struct {
		Shortname string
		Icons     []struct {
			Src  string
			Size string
			Type string
		}
	}{
		Shortname: source.name,
	}

	assetDirectoryListing, err := assets.ReadDir(config.assets.assetsDir)
	if err != nil {
		log.Println("couldn't open embedded assets:", err.Error())
		exit(1)
	}

	re := regexp.MustCompile(`^icon`)

	for _, entry := range assetDirectoryListing {
		if !entry.IsDir() {
			filename := filepath.Base(entry.Name())
			// check if asset filename starts with the string "icon"
			if re.MatchString(filename) {
				iconSize, err := getIconSize(filename)
				if err != nil {
					log.Println("couldn't define icon size:", err.Error())
					exit(1)
				}

				iconType, err := getIconType(filename)
				if err != nil {
					log.Println("couldn't define icon type:", err.Error())
					exit(1)
				}

				PWAData.Icons = append(PWAData.Icons, struct {
					Src  string
					Size string
					Type string
				}{
					Src:  filename,
					Size: iconSize,
					Type: iconType,
				})
			}
		}
	}

	manifestFilePath := filepath.Join(gallery.absPath, config.assets.manifestFile)
	if dryRun {
		log.Println("Would create web app manifest file:", manifestFilePath)
	} else {
		templatePath := filepath.Join(config.assets.assetsDir, config.assets.manifestTemplate)
		cookedTemplate, err := template.ParseFS(assets, templatePath)
		if err != nil {
			log.Println("couldn't parse manifest template", templatePath, ":", err.Error())
			exit(1)
		}

		manifestFileHandle, err := os.Create(manifestFilePath)
		if err != nil {
			log.Println("couldn't create manifest file", manifestFilePath, ":", err.Error())
			exit(1)
		}

		err = cookedTemplate.Execute(manifestFileHandle, PWAData)
		if err != nil {
			log.Println("couldn't execute manifest template", manifestFilePath, ":", err.Error())
			exit(1)
		}

		manifestFileHandle.Sync()
		manifestFileHandle.Close()

		log.Println("Created manifest file:", manifestFilePath)
	}
}

// copyRootAssets copies all the embedded assets to the root directory of the gallery
func copyRootAssets(gallery directory, dryRun bool, config configuration) {
	assetDirectoryListing, err := assets.ReadDir(config.assets.assetsDir)
	if err != nil {
		log.Println("couldn't open embedded assets:", err.Error())
		exit(1)
	}

	// Iterate through all the embedded assets
	// TODO only update assets if they're not up to date
	// TODO then add logging for created assets
	for _, entry := range assetDirectoryListing {
		if !entry.IsDir() {
			switch filepath.Ext(strings.ToLower(entry.Name())) {
			// Copy all javascript and CSS files
			case ".js", ".css", ".png":
				if dryRun {
					log.Println("Would copy JS/CSS/PNG file", entry.Name(), "to", gallery.absPath)
				} else {
					if entry.Name() == config.assets.playIcon {
						break
					}

					assetPath := filepath.Join(config.assets.assetsDir, entry.Name())
					filebuffer, err := assets.ReadFile(assetPath)
					if err != nil {
						log.Println("couldn't open embedded asset:", assetPath, ":", err.Error())
						exit(1)
					}
					targetPath := filepath.Join(gallery.absPath, entry.Name())
					err = os.WriteFile(targetPath, filebuffer, config.files.fileMode)
					if err != nil {
						log.Println("couldn't write embedded asset:", targetPath, ":", err.Error())
						exit(1)
					}
				}
			}
		}
	}
}

// createHTML creates an HTML file in the gallery directory, by filling in the thisHTML struct
// with all the required information, combining it with the HTML template and saving it in the file
func createHTML(depth int, source directory, galleryDirectory string, dryRun bool, config configuration) {
	// create the thisHTML struct and start filling it with the relevant data
	var thisHTML htmlData

	// The page title will be the directory name
	thisHTML.Title = source.name

	// Go through each directory and file and add them to the slices
	for _, subdir := range source.subdirectories {
		thisHTML.Subdirectories = append(thisHTML.Subdirectories, subdir.name)
	}
	for _, file := range source.files {
		thumbnailFilename, fullsizeFilename := getGalleryFilenames(file.name, config)
		thisHTML.Files = append(thisHTML.Files, struct {
			Filename  string
			Thumbnail string
			Fullsize  string
			Original  string
		}{
			Filename:  file.name,
			Thumbnail: filepath.Join(config.files.thumbnailDir, thumbnailFilename),
			Fullsize:  filepath.Join(config.files.fullsizeDir, fullsizeFilename),
			Original:  filepath.Join(config.files.originalDir, file.name),
		})
	}

	// We'll use relative paths to refer to the root direct assets such as icons, JS and CSS.
	// The depth parameter is used to figure out how deep in a subdirectory we are
	rootEscape := ""
	for i := 0; i < depth; i = i + 1 {
		rootEscape = rootEscape + "../"
	}

	assetDirectoryListing, err := assets.ReadDir(config.assets.assetsDir)
	if err != nil {
		log.Println("couldn't list embedded assets:", err.Error())
		exit(1)
	}

	// Go through the embedded assets and add all JS and CSS files, link them
	for _, entry := range assetDirectoryListing {
		if !entry.IsDir() {
			switch filepath.Ext(strings.ToLower(entry.Name())) {
			// Copy all javascript and CSS files
			case ".js":
				thisHTML.JS = append(thisHTML.JS, filepath.Join(rootEscape, entry.Name()))
			case ".css":
				thisHTML.CSS = append(thisHTML.CSS, filepath.Join(rootEscape, entry.Name()))
			}
		}
	}

	// If we're not in the root directory, link the back icon and show it in the HTML page
	if depth > 0 {
		thisHTML.BackIcon = filepath.Join(rootEscape, config.assets.backIcon)
	}
	// Generic folder icon to be used for each subfolder
	thisHTML.FolderIcon = filepath.Join(rootEscape, config.assets.folderIcon)

	// thisHTML struct has been filled in successfully, parse the HTML template,
	// fill in the data and write it to the correct file
	htmlFilePath := filepath.Join(galleryDirectory, config.assets.htmlFile)
	if dryRun {
		log.Println("Would create HTML file:", htmlFilePath)
	} else {
		templatePath := filepath.Join(config.assets.assetsDir, config.assets.htmlTemplate)
		cookedTemplate, err := template.ParseFS(assets, templatePath)
		if err != nil {
			log.Println("couldn't parse HTML template", templatePath, ":", err.Error())
			exit(1)
		}

		htmlFileHandle, err := os.Create(htmlFilePath)
		if err != nil {
			log.Println("couldn't create HTML file", htmlFilePath, ":", err.Error())
			exit(1)
		}

		err = cookedTemplate.Execute(htmlFileHandle, thisHTML)
		if err != nil {
			log.Println("couldn't execute HTML template", htmlFilePath, ":", err.Error())
			exit(1)
		}

		htmlFileHandle.Sync()
		htmlFileHandle.Close()

		log.Println("Created HTML file:", htmlFilePath)
	}
}

// getGalleryDirectoryNames parses the names for subdirectories for thumbnail, full size
// and original pictures in the gallery directory
func getGalleryDirectoryNames(galleryDirectory string, config configuration) (thumbnailGalleryDirectory string, fullsizeGalleryDirectory string, originalGalleryDirectory string) {
	thumbnailGalleryDirectory = filepath.Join(galleryDirectory, config.files.thumbnailDir)
	fullsizeGalleryDirectory = filepath.Join(galleryDirectory, config.files.fullsizeDir)
	originalGalleryDirectory = filepath.Join(galleryDirectory, config.files.originalDir)
	return
}

func transformImage(source string, fullsizeDestination string, thumbnailDestination string, config configuration) error {
	if config.files.imageExtension == ".jpg" {
		// First create full-size image
		image, err := vips.NewImageFromFile(source)
		if err != nil {
			log.Println("couldn't open full-size image:", source, err.Error())
			return err
		}

		err = image.AutoRotate()
		if err != nil {
			log.Println("couldn't autorotate full-size image:", source, err.Error())
			return err
		}

		// Calculate the scaling factor used to make the image smaller
		scale := float64(config.media.fullsizeMaxWidth) / float64(image.Width())
		if (scale * float64(image.Height())) > float64(config.media.fullsizeMaxHeight) {
			// If the image is tall vertically, use height instead of width to recalculate scaling factor
			scale = float64(config.media.fullsizeMaxHeight) / float64(image.Height())
		}

		// TODO don't enlarge the file by accident
		err = image.Resize(scale, vips.KernelAuto)
		if err != nil {
			log.Println("couldn't resize full-size image:", source, err.Error())
			return err
		}

		ep := vips.NewDefaultJPEGExportParams()
		fullsizeBuffer, _, err := image.Export(ep)
		if err != nil {
			log.Println("couldn't export full-size image:", source, err.Error())
			return err
		}

		err = os.WriteFile(fullsizeDestination, fullsizeBuffer, config.files.fileMode)
		if err != nil {
			log.Println("couldn't write full-size image:", fullsizeDestination, err.Error())
			return err
		}

		// After full-size image, create thumbnail
		err = image.Thumbnail(config.media.thumbnailWidth, config.media.thumbnailHeight, vips.InterestingAttention)
		if err != nil {
			log.Println("couldn't crop thumbnail:", err.Error())
			return err
		}

		thumbnailBuffer, _, err := image.Export(ep)
		if err != nil {
			log.Println("couldn't export thumbnail image:", source, err.Error())
			return err
		}

		err = os.WriteFile(thumbnailDestination, thumbnailBuffer, config.files.fileMode)
		if err != nil {
			log.Println("couldn't write thumbnail image:", thumbnailDestination, err.Error())
			return err
		}
	} else {
		log.Println("Can't figure out what format to convert full size image to:", source)
		return errors.New("invalid target format for full-size image")
	}

	return nil
}

func transformVideo(source string, fullsizeDestination string, thumbnailDestination string, config configuration) error {
	// Resize full-size video
	ffmpegCommand := exec.Command("ffmpeg", "-y", "-i", source, "-pix_fmt", "yuv420p", "-vcodec", "libx264", "-acodec", "aac", "-movflags", "faststart", "-r", "24", "-vf", "scale='min("+strconv.Itoa(config.media.videoMaxSize)+",iw)':'min("+strconv.Itoa(config.media.videoMaxSize)+",ih)':force_original_aspect_ratio=decrease:force_divisible_by=2", "-crf", "28", "-loglevel", "error", fullsizeDestination)

	commandOutput, err := ffmpegCommand.CombinedOutput()
	if err != nil {
		log.Println("Could not get ffmpeg fullsize output:", err)
	}

	if len(commandOutput) > 0 {
		log.Println("ffmpeg output for fullsize operation:", source)
		log.Println(ffmpegCommand.Args)
		log.Println(string(commandOutput))
	}

	if err != nil {
		return err
	}

	// Create thumbnail image of video
	ffmpegCommand2 := exec.Command("ffmpeg", "-y", "-i", source, "-ss", "00:00:00", "-vframes", "1", "-vf", fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=increase:force_divisible_by=2,crop=%d:%d", config.media.thumbnailWidth, config.media.thumbnailHeight, config.media.thumbnailWidth, config.media.thumbnailHeight), "-loglevel", "error", thumbnailDestination)

	commandOutput2, err := ffmpegCommand2.CombinedOutput()
	if err != nil {
		log.Println("Could not get ffmpeg thumbnail output:", err)
	}

	if len(commandOutput2) > 0 {
		log.Println("ffmpeg output for thumbnail operation:", source)
		log.Println(ffmpegCommand2.Args)
		log.Println(string(commandOutput2))
	}

	if err != nil {
		return err
	}

	// Take thumbnail and overlay triangle image on top of it
	image, err := vips.NewImageFromFile(thumbnailDestination)
	if err != nil {
		log.Println("Could not open video thumbnail:", thumbnailDestination)
		return err
	}

	playbuttonAssetPath := filepath.Join(config.assets.assetsDir, config.assets.playIcon)
	playbuttonOverlayBuffer, err := assets.ReadFile(playbuttonAssetPath)
	if err != nil {
		log.Println("Could not read play button overlay asset")
		return err
	}
	playbuttonOverlayImage, err := vips.NewImageFromBuffer(playbuttonOverlayBuffer)
	if err != nil {
		log.Println("Could not open play button overlay asset")
		return err
	}

	// Overlay play button in the middle of thumbnail picture
	err = image.Composite(playbuttonOverlayImage, vips.BlendModeOver, (config.media.thumbnailWidth/2)-(playbuttonOverlayImage.Width()/2), (config.media.thumbnailHeight/2)-(playbuttonOverlayImage.Height()/2))
	if err != nil {
		log.Println("Could not composite play button overlay on top of:", thumbnailDestination)
		return err
	}

	ep := vips.NewDefaultJPEGExportParams()
	imageBytes, _, err := image.Export(ep)
	if err != nil {
		log.Println("Could not export video thumnail:", thumbnailDestination)
		return err
	}

	err = os.WriteFile(thumbnailDestination, imageBytes, config.files.fileMode)
	if err != nil {
		log.Println("Could not write video thumnail:", thumbnailDestination)
		return err
	}

	return nil
}

func createOriginal(source string, destination string) error {
	// TODO add option to copy
	return symlinkFile(source, destination)
}

func getGalleryFilenames(sourceFilename string, config configuration) (thumbnailFilename string, fullsizeFilename string) {
	thumbnailFilename = stripExtension(sourceFilename) + config.files.imageExtension
	if isImageFile(sourceFilename) {
		fullsizeFilename = stripExtension(sourceFilename) + config.files.imageExtension
	} else if isVideoFile(sourceFilename) {
		fullsizeFilename = stripExtension(sourceFilename) + config.files.videoExtension
	} else {
		log.Println("could not infer whether file is image or video:", sourceFilename)
		exit(1)
	}
	return
}

func cleanWipFiles(sourceFilepath string) {
	wipJobMutex.Lock()
	os.Remove(wipJobs[sourceFilepath].thumbnailFilepath)
	os.Remove(wipJobs[sourceFilepath].fullsizeFilepath)
	os.Remove(wipJobs[sourceFilepath].originalFilepath)
	delete(wipJobs, sourceFilepath)
	wipJobMutex.Unlock()
}

// transformFile takes a transformation job (an image or video) and creates a thumbnail, full-size
// image and a copy of the original
func transformFile(thisJob transformationJob, progressBar *pb.ProgressBar, config configuration) {
	// Before we begin work, add all work-in-progress files to wipSlice
	// In case the program is killed before we're finished, signalHandler() deletes all the wip files.
	// This way, no half-finished files will stay on the hard drive
	wipJobMutex.Lock()
	wipJobs[thisJob.sourceFilepath] = thisJob
	wipJobMutex.Unlock()

	// Do the actual transformation and increment the progress bar
	if isImageFile(thisJob.filename) {
		err := transformImage(thisJob.sourceFilepath, thisJob.fullsizeFilepath, thisJob.thumbnailFilepath, config)
		if err != nil {
			cleanWipFiles(thisJob.sourceFilepath)
			if progressBar != nil {
				progressBar.Increment()
			}
			return
		}
	} else if isVideoFile(thisJob.filename) {
		err := transformVideo(thisJob.sourceFilepath, thisJob.fullsizeFilepath, thisJob.thumbnailFilepath, config)
		if err != nil {
			cleanWipFiles(thisJob.sourceFilepath)
			if progressBar != nil {
				progressBar.Increment()
			}
			return
		}
	} else {
		log.Println("could not infer whether file is image or video(2):", thisJob.sourceFilepath)
		exit(1)
	}
	err := createOriginal(thisJob.sourceFilepath, thisJob.originalFilepath)
	if err != nil {
		cleanWipFiles(thisJob.sourceFilepath)
		if progressBar != nil {
			progressBar.Increment()
		}
		return
	}
	if progressBar != nil {
		progressBar.Increment()
	}

	wipJobMutex.Lock()
	delete(wipJobs, thisJob.sourceFilepath)
	wipJobMutex.Unlock()

	log.Println("Converted media file:", thisJob.sourceFilepath)
}

// This is the main concurrent goroutine that takes care of the parallelisation. A big bunch of them
// are created in a worker pool and they're fed new images/videos to transform via a channel.
func transformationWorker(thisDirectoryWG *sync.WaitGroup, thisDirectoryJobs chan transformationJob, progressBar *pb.ProgressBar, config configuration) {
	defer thisDirectoryWG.Done()
	for thisJob := range thisDirectoryJobs {
		transformFile(thisJob, progressBar, config)
		runtime.GC()
	}
}

// createMedia takes the source directory, and creates a thumbnail, full-size
// version and original of each non-existing file to the respective gallery directory.
func createMedia(source directory, gallerySubdirectory string, dryRun bool, config configuration, progressBar *pb.ProgressBar) {
	thumbnailGalleryDirectory, fullsizeGalleryDirectory, originalGalleryDirectory := getGalleryDirectoryNames(gallerySubdirectory, config)

	// Create subdirectories in gallery directory for thumbnails, full-size and original pics
	createDirectory(thumbnailGalleryDirectory, dryRun, config.files.directoryMode)
	createDirectory(fullsizeGalleryDirectory, dryRun, config.files.directoryMode)
	createDirectory(originalGalleryDirectory, dryRun, config.files.directoryMode)

	// This is the concurrency part of the function. Set up a worker pool, channel to communicate with them,
	// and a wait group to block in the end.
	thisDirectoryJobs := make(chan transformationJob, 10000)
	var thisDirectoryWG sync.WaitGroup
	for i := 1; i <= config.concurrency; i = i + 1 {
		thisDirectoryWG.Add(1)
		go transformationWorker(&thisDirectoryWG, thisDirectoryJobs, progressBar, config)
	}
	// Here ends the concurrency code. Below we loop through the files, pushing them as
	// new jobs via the channel to the worker pool, and in the end of the function we
	// have code to wrap-up the concurrency.

	for _, file := range source.files {
		if !file.exists {
			var thisJob transformationJob
			thisJob.filename = file.name
			thisJob.sourceFilepath = filepath.Join(source.absPath, file.name)
			thumbnailFilename, fullsizeFilename := getGalleryFilenames(file.name, config)
			thisJob.thumbnailFilepath = filepath.Join(thumbnailGalleryDirectory, thumbnailFilename)
			thisJob.fullsizeFilepath = filepath.Join(fullsizeGalleryDirectory, fullsizeFilename)
			thisJob.originalFilepath = filepath.Join(originalGalleryDirectory, file.name)

			if dryRun {
				log.Println("Would convert:", thisJob.sourceFilepath, thisJob.thumbnailFilepath, thisJob.fullsizeFilepath, thisJob.originalFilepath)
			} else {
				thisDirectoryJobs <- thisJob
			}
		}
	}

	// Here we have the tail end of the concurrency code. The main thread blocks here to wait
	// for all the workers to have transformed all the image and video jobs given to them in the loop
	// above. We close the channel to clarify to the workers there's no more stuff to do.
	close(thisDirectoryJobs)
	thisDirectoryWG.Wait()
}

// cleanUp cleans stale files and directories from the gallery recursively
func cleanUp(gallery directory, dryRun bool, config configuration) {
	cleanDirectory(gallery, dryRun, config)

	for _, subdir := range gallery.subdirectories {
		cleanUp(subdir, dryRun, config)
	}
}

// Clean gallery directory of any directories or files which don't exist in source
func cleanDirectory(gallery directory, dryRun bool, config configuration) {
	for _, file := range gallery.files {
		if !file.exists && !reservedFile(file.name, config) {
			stalePath := filepath.Join(gallery.absPath, file.name)
			if dryRun {
				log.Println("would clean up file:", stalePath)
			} else {
				err := os.RemoveAll(stalePath)
				if err != nil {
					log.Println("couldn't delete stale gallery file", stalePath, ":", err.Error())
				}
				log.Println("Cleaned up file:", stalePath)
			}
		}
	}

	for _, dir := range gallery.subdirectories {
		if !reservedDirectory(dir.name, config) && !dir.exists {
			stalePath := filepath.Join(gallery.absPath, dir.name)
			if dryRun {
				log.Println("would clean up dir:", stalePath)
			} else {
				err := os.RemoveAll(stalePath)
				if err != nil {
					log.Println("couldn't delete stale gallery directory", stalePath, ":", err.Error())
				}
				log.Println("Cleaned up directory:", stalePath)
			}
		}
	}
}

func updateHTMLFiles(depth int, source directory, gallery directory, dryRun bool, cleanUp bool, config configuration) {
	galleryDirectory := filepath.Join(gallery.absPath, source.relPath)
	if hasDirectoryChanged(source, gallery, cleanUp, config) {
		createHTML(depth, source, galleryDirectory, dryRun, config)
	}

	for _, subdir := range source.subdirectories {
		updateHTMLFiles(depth+1, subdir, gallery, dryRun, cleanUp, config)
	}
}

func updateMediaFiles(depth int, source directory, gallery directory, dryRun bool, cleanUp bool, config configuration, progressBar *pb.ProgressBar) {
	// TODO generalize directory recursion algorithm for media creation, HTML creation and clean-ups
	// TODO make generalized function recurse simultaneously source and gallery structs
	galleryDirectory := filepath.Join(gallery.absPath, source.relPath)

	if hasDirectoryChanged(source, gallery, cleanUp, config) {
		createMedia(source, galleryDirectory, dryRun, config, progressBar)
	}

	for _, subdir := range source.subdirectories {
		// Create respective source subdirectory also in gallery subdirectory
		gallerySubdir := filepath.Join(gallery.absPath, subdir.relPath)
		createDirectory(gallerySubdir, dryRun, config.files.directoryMode)

		// Recurse
		updateMediaFiles(depth+1, subdir, gallery, dryRun, cleanUp, config, progressBar)
	}
}

func setupSignalHandler() {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	go signalHandler(signalChan)
}

func signalHandler(signalChan chan os.Signal) {
	<-signalChan
	log.Println("Ctrl-C received, cleaning up and aborting...")
	wipJobMutex.Lock()
	for _, job := range wipJobs {
		os.Remove(job.thumbnailFilepath)
		os.Remove(job.fullsizeFilepath)
		os.Remove(job.originalFilepath)
	}
	exit(0)
}

func main() {
	// Define command-line arguments
	var args struct {
		Source   string `arg:"positional,required" help:"Source directory for images/videos"`
		Gallery  string `arg:"positional,required" help:"Destination directory to create gallery in"`
		Verbose  bool   `arg:"-v,--verbose" help:"verbosity level"`
		DryRun   bool   `arg:"--dry-run" help:"dry run; don't change anything, just print what would be done"`
		CleanUp  bool   `arg:"-c,--cleanup" help:"cleanup, delete files and directories in gallery which don't exist in source"`
		NoVideos bool   `arg:"--no-videos" help:"ignore videos, only include images"`
		Logfile  string `arg:"-l,--log" help:"recommended: log file to save errors and failed filenames to instead of stdout"`
	}
	// TODO implement verbose
	// TODO fix stdout vs logging output throughout

	// Parse command-line arguments
	arg.MustParse(&args)

	// Validate source and gallery arguments, make paths absolute
	args.Source, args.Gallery = validateSourceAndGallery(args.Source, args.Gallery)

	// Initialize configuration (assets, directories, file types)
	config := initializeConfig()

	// Open log file if parameter provided
	if args.Logfile != "" {
		fmt.Println("Logfile:", args.Logfile)
		logHandle, err := os.OpenFile(args.Logfile, os.O_RDWR|os.O_CREATE|os.O_APPEND, config.files.fileMode)
		if err != nil {
			fmt.Println("error opening logfile:", args.Logfile)
			exit(1)
		}
		defer logHandle.Close()
		log.SetOutput(logHandle)
	}

	fmt.Println("Creating gallery, source:", args.Source, "gallery:", args.Gallery)
	fmt.Println("Finding all media files...")

	// Creating a directory struct of both source as well as gallery directories
	source := createDirectoryTree(args.Source, "", args.NoVideos)
	gallery := createDirectoryTree(args.Gallery, "", args.NoVideos)

	// Check which source media exists in gallery
	compareDirectoryTrees(&source, &gallery, config)

	// If there are changes in the source, update the media files
	newSourceFiles := countChanges(source, config)

	if newSourceFiles > 0 {
		log.Println("Updating", newSourceFiles, "media files.")
		if !exists(gallery.absPath) {
			createDirectory(gallery.absPath, args.DryRun, config.files.directoryMode)
		}

		var progressBar *pb.ProgressBar
		if !args.DryRun {
			progressBar = pb.StartNew(newSourceFiles)
			if args.Verbose {
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

		// Copy updated web assets (JS, CSS, icons, etc) into gallery root
		copyRootAssets(gallery, args.DryRun, config)

		// Copy PWA web manifest and fill-in relevant details
		createPWAManifest(gallery, source, args.DryRun, config)

		// Handle ctrl-C or other signals
		setupSignalHandler()

		updateMediaFiles(0, source, gallery, args.DryRun, args.CleanUp, config, progressBar)

		if !args.DryRun {
			progressBar.Finish()
		}

		fmt.Println("All media files updated!")
	} else {
		fmt.Println("All media files already up to date!")
	}

	// Update HTML index files, if any new source media files, removed gallery media files
	// or missing HTML files
	staleGalleryFiles := countChanges(gallery, config)
	missingHTMLFiles := findMissingHTMLFiles(gallery, config)

	if newSourceFiles > 0 || staleGalleryFiles > 0 || missingHTMLFiles {
		fmt.Println("Updating HTML files...")
		updateHTMLFiles(0, source, gallery, args.DryRun, args.CleanUp, config)
		fmt.Println("All HTML files updated!")
	} else {
		fmt.Println("All HTML files already up to date!")
	}

	// Clean up any removed gallery media files
	if args.CleanUp {
		fmt.Println("Cleaning up gallery...")
		// TODO restructure cleanUp to check here whether there's stale files, for better output
		cleanUp(gallery, args.DryRun, config)
		fmt.Println("Gallery clean!")
	}
}
