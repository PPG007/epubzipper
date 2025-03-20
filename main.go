package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"github.com/disintegration/imaging"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
	"image"
	"io"
	"math"
	"os"
	"path"
	"path/filepath"
	"strings"
)

var (
	src           string
	out           string
	ratio         float64
	newFileSuffix = "_zipped"
)

var rootCmd = &cobra.Command{
	Use: "epubZipper",
	RunE: func(cmd *cobra.Command, args []string) error {
		pwd, err := os.Getwd()
		if err != nil {
			return err
		}
		srcPath, srcInfo, err := getSrcPath(pwd)
		if err != nil {
			return err
		}
		outPath, err := getOutPath(pwd, srcPath, srcInfo)
		if err != nil {
			return err
		}
		var files []string
		if srcInfo.IsDir() {
			entries, err := os.ReadDir(srcPath)
			if err != nil {
				return err
			}
			for _, entry := range entries {
				if !entry.IsDir() {
					files = append(files, path.Join(srcPath, entry.Name()))
				}
			}
		} else {
			files = append(files, srcPath)
		}
		for _, file := range files {
			if err := processFile(file, outPath); err != nil {
				return err
			}
		}
		return nil
	},
}

func getSrcPath(pwd string) (string, os.FileInfo, error) {
	srcPath := src
	if !path.IsAbs(srcPath) {
		srcPath = path.Join(pwd, src)
	}
	info, err := os.Stat(srcPath)
	if err != nil {
		return "", nil, err
	}
	return srcPath, info, nil
}

func getOutPath(pwd, srcPath string, srcInfo os.FileInfo) (string, error) {
	if out == "" {
		if srcInfo.IsDir() {
			return srcPath, nil
		} else {
			return path.Dir(srcPath), nil
		}
	}
	if !path.IsAbs(out) {
		out = path.Join(pwd, out)
	}
	outInfo, err := os.Stat(out)
	if err != nil {
		return "", err
	}
	if !outInfo.IsDir() {
		return "", errors.New("invalid output path")
	}
	return out, nil
}

func processFile(filePath, outPath string) error {
	fmt.Printf("processing %s...\n", filePath)
	outDir, err := unzipEpub(filePath, outPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = cleanFiles(outDir)
	}()
	err = processImages(path.Join(outDir, "images"))
	if err != nil {
		return err
	}
	return zipEpub(outDir)
}

func unzipEpub(filePath, outPath string) (string, error) {
	fmt.Println("unzipping...")
	info, err := os.Stat(filePath)
	if err != nil {
		return "", err
	}
	outDir := path.Join(outPath, strings.TrimRight(info.Name(), ".epub"))
	_ = os.RemoveAll(outDir)
	err = os.Mkdir(outDir, os.ModePerm)
	if err != nil {
		return "", err
	}
	reader, err := zip.OpenReader(filePath)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = reader.Close()
	}()
	for _, f := range reader.File {
		outPath := filepath.Join(outDir, f.Name)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(outPath, os.ModePerm); err != nil {
				return "", err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(outPath), os.ModePerm); err != nil {
			return "", err
		}
		dst, err := os.OpenFile(outPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return "", err
		}
		archiveFile, err := f.Open()
		if err != nil {
			return "", err
		}
		if _, err := io.Copy(dst, archiveFile); err != nil {
			return "", err
		}
		_ = dst.Close()
		_ = archiveFile.Close()
	}
	return outDir, nil
}

func processImages(imagePath string) error {
	entries, err := os.ReadDir(imagePath)
	if err != nil {
		return err
	}
	bar := progressbar.Default(int64(len(entries)), "resizing")
	defer func() {
		_ = bar.Finish()
	}()
	for _, entry := range entries {
		_ = bar.Add(1)
		if entry.IsDir() {
			continue
		}
		imgPath := path.Join(imagePath, entry.Name())
		img, err := imaging.Open(imgPath)
		if err != nil {
			return err
		}
		err = os.Remove(path.Join(imagePath, entry.Name()))
		if err != nil {
			return err
		}
		err = imaging.Save(resizeImage(img), imgPath)
		if err != nil {
			return err
		}
	}
	return nil
}

func zipEpub(dir string) error {
	fmt.Println("zipping...")
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	filePath := path.Join(path.Dir(dir), fmt.Sprintf("%s%s.epub", info.Name(), newFileSuffix))
	_ = os.Remove(filePath)
	zipFile, err := os.Create(filePath)
	if err != nil {
		return err
	}
	writer := zip.NewWriter(zipFile)
	defer func() {
		_ = writer.Flush()
		_ = writer.Close()
		_ = zipFile.Sync()
		_ = zipFile.Close()
	}()
	err = addFilesToZip(writer, dir, "")
	if err != nil {
		return err
	}
	return nil
}

func addFilesToZip(writer *zip.Writer, basePath, baseInZip string) error {
	entries, err := os.ReadDir(basePath)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		filePath := path.Join(basePath, entry.Name())
		zipPath := path.Join(baseInZip, entry.Name())
		if entry.IsDir() {
			err := addFilesToZip(writer, filePath, zipPath)
			if err != nil {
				return err
			}
		} else {
			file, err := os.Open(filePath)
			if err != nil {
				return err
			}
			info, err := file.Stat()
			if err != nil {
				return err
			}
			header, err := zip.FileInfoHeader(info)
			if err != nil {
				return err
			}
			header.Name = zipPath
			header.Method = zip.Deflate
			writerFile, err := writer.CreateHeader(header)
			if err != nil {
				return err
			}
			_, err = io.Copy(writerFile, file)
			if err != nil {
				return err
			}
			_ = file.Close()
		}
	}
	return err
}

func cleanFiles(dir string) error {
	fmt.Println("cleaning...")
	return os.RemoveAll(dir)
}

func resizeImage(img image.Image) image.Image {
	width := math.Floor(float64(img.Bounds().Dx()) * ratio)
	height := math.Floor(float64(img.Bounds().Dy()) * ratio)
	return imaging.Resize(img, int(width), int(height), imaging.Lanczos)
}

func init() {
	rootCmd.Flags().StringVarP(&src, "src", "s", ".", "path of epub source file")
	rootCmd.Flags().StringVarP(&out, "out", "o", "", "path of output file, default is src path")
	rootCmd.Flags().Float64VarP(&ratio, "ratio", "r", 0.5, "ratio of epub file size")
}

func main() {
	err := rootCmd.Execute()
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
	os.Exit(0)
}
