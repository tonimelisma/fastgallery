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

For Ubuntu 20.04 focal, first add my PPA for latest libvips with HEIF support:

`sudo add-apt-repository ppa:tonimelisma/ppa`

Then, for dependencies, install libvips42 for images and optionally ffmpeg (if you need video support):

`apt-get install libvips42 ffmpeg`

## Usage

`fastgallery -o /var/www/html ~/Dropbox/Pictures`

## Roadmap

For the prioritised roadmap, please see https://github.com/tonimelisma/fastgallery/projects/1
