# fastgallery - Static photo gallery generator

[![Build Status](https://travis-ci.com/tonimelisma/fastgallery.svg?branch=master)](https://travis-ci.com/tonimelisma/fastgallery)

Creates a static gallery of your photo and video library.

- Super fast (written completely in Go, concurrent, uses fastest image/video libraries, 4-8 times faster than others)
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

Then, for dependencies, install libvips42 for images and optionally ffmpeg (if you need video support)

`apt-get install libvips42 ffmpeg`

Image and video format support will depend on the support compiled in these libraries.

## Usage

`VIPS_WARNING=0 fastgallery -o /var/www/html ~/Dropbox/Pictures`

## Backlog

Before 0.1 Alpha release, still to do:
- Convert thumbnail and full-size pictures
- Add triangle overlay on video thumbnails to indicate video
- Clean up half-finished thumbnail/fullsize/symlink if program is halted midway
- Use all of thumb/full/symlink in detecting changes required

Before 0.1 Beta release:
- Clean function names
- Refactor functions into internal packages
- Create unit tests (blargh)
- Packaging for Ubuntu
- Set up Ubuntu repository (Github? PPA?)
- Finger swiping for web frontend
- Arrow key navigation for web frontend

Other stuff on the roadmap:
- Allow copying instead of symlinking originals
- Lots of options / config file to tweak defaults
- Patch bimg library so it doesn't log to console without VIPS_WARNING (https://github.com/h2non/bimg/issues/355)
- Add logging to file, better bimg and ffmpeg error handling, when to panic
- Add 'force_divisible_by=2' to ffmpeg encoding (when feature is available in next ffmpeg release)
- Go through the rest of the minor annoyances (TODOs in code)
