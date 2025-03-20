# epubzipper

A simple tool to decrease epub file size so that it can be sent by amazon "send to kindle".

This tool will decrease epub file size by resizing images.

## Usage

basic usage:

```shell
# process single file
epubzipper -s input.epub
# process files in a directory
epubzipper -s library
```

customize output path, which is default to the source directory:

```shell
epubzipper -s input.epub -o output
```

customize image resize ratio, which is default to 0.5:

```shell
epubzipper -s input.epub -r 0.1
```