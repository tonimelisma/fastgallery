# gogallery - Static photo gallery generator

- Both photo and video support
- Deals with any file formats (ncluding HEIC, HEVC)
- Only updates changed files
- Uses relative paths (safe for subdirectories or S3)

N.B. deletes all unused media files in output directory

## Install

Ubuntu
apt-get install libvips42

## Usage

VIPS_WARNING=0 gogallery -o /var/www/html ~/Dropbox/Pictures

## FAQ

Why do I need to set the VIPS_WARNING environment variable to suppress warnings?

Unfortunately the excellent bimg image manipulation library doesn't have full support for the underlying VIPS library and doesn't have capabilities to properly manage its logging. See issue XXX
