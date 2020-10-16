# fastgallery - Static photo gallery generator

[![Build Status](https://travis-ci.com/tonimelisma/fastgallery.svg?branch=master)](https://travis-ci.com/tonimelisma/fastgallery)

Creates a static gallery of your photo and video library.

- Super fast (written in Go and C, concurrent, uses fastest image/video libraries, 4-8 times faster than others)
- Both photo and video support
- Deals with any file formats (ncluding HEIC, HEVC)
- Only updates changed files, runs incrementally
- Uses relative paths (safe for using in subdirectory or S3)

*Please note that fastgallery is still pre-alpha, I am actively working on it*

## Install

### MacOS

For dependencies, use Homebrew to install:

`brew install vips ffmpeg`

### Ubuntu Linux

If you want HEIF support, be sure to first add my PPA with updated libvips42 packages:

`sudo add-apt-repository ppa:tonimelisma/ppa`

Then, for dependencies, install libvips42 for images and optionally ffmpeg (if you need video support):

`apt-get install libvips42 ffmpeg`

## Usage

`VIPS_WARNING=0 fastgallery -o /var/www/html ~/Dropbox/Pictures`

## Backlog

Before 0.1 Alpha release, still to do:
- Finalize web frontend so it looks sleek
- Optimize image conversion from three steps into one
- Add triangle overlay on video thumbnails to indicate video

Before 0.1 Beta release:
- Use all of thumb/full/symlink in detecting changes required
- Patch bimg library so it doesn't log to console without VIPS_WARNING (https://github.com/h2non/bimg/issues/355)
- Clean function and variable names
- Refactor functions into internal packages
- Create unit tests (blargh)
- Check if half-finished thumbnail/fullsize images/videos create corrupt files if program is interrupted

Before 0.1 final release:
- Packaging for Ubuntu in my PPA
- Packaging for Homebrew / MacOS

Other stuff on the roadmap:
- Finger swiping for web frontend
- Arrow key navigation for web frontend
- Allow copying instead of symlinking originals
- Lots of options / config file to tweak defaults
- Add logging to file, better bimg and ffmpeg error handling, flag to panic on warnings
- Add 'force_divisible_by=2' to ffmpeg encoding (when feature is available in next ffmpeg release)
- Go through the rest of the minor annoyances (TODOs in code)
