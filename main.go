package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"fortio.org/progressbar"
	"github.com/disintegration/imaging"
	"github.com/panjf2000/ants/v2"
	"github.com/spf13/cobra"
	"image"
	"io"
	"math"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

var (
	src             string
	out             string
	ratio           float64
	newFileSuffix   = "_zipped"
	concurrency     int
	maxPrefixLength = 15
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
		files, err := getFiles(srcPath, srcInfo)
		if err != nil {
			return err
		}
		if len(files) == 0 {
			return errors.New("no files found")
		}
		wg := &sync.WaitGroup{}
		pool, err := ants.NewPool(concurrency)
		if err != nil {
			return err
		}
		defer pool.Release()
		fmt.Println(progressbar.ClearAfter)
		cfg := progressbar.DefaultConfig()
		cfg.ScreenWriter = os.Stdout
		cfg.UpdateInterval = 0
		cfg.ExtraLines = 1
		multiBar := cfg.NewMultiBarPrefixes(formatPrefix(strings.TrimLeft(files[0], path.Dir(files[0]))))
		multiBar.PrefixesAlign()
		firstBar := multiBar.Bars[0]
		defer func() {
			multiBar.End()
		}()
		for i, file := range files {
			temp := file
			index := i
			wg.Add(1)
			err = pool.Submit(func() {
				defer wg.Done()
				prefix := formatPrefix(strings.TrimLeft(temp, path.Dir(temp)))
				cfg.Prefix = prefix
				var subBar *progressbar.Bar
				if index == 0 {
					subBar = firstBar
				} else {
					subBar = cfg.NewBar()
					multiBar.Add(subBar)
				}
				multiBar.PrefixesAlign()
				innerErr := processFile(temp, outPath, subBar)
				if innerErr != nil {
					subBar.WriteAbove(formatAbove(innerErr.Error()))
				} else {
					subBar.WriteAbove(formatAbove("finished."))
					subBar.Progress(100)
				}
			})
			if err != nil {
				return err
			}
		}
		wg.Wait()
		return nil
	},
}

func formatPrefix(prefix string) string {
	if len([]rune(prefix)) > maxPrefixLength {
		prefix = fmt.Sprintf("%s...", string([]rune(prefix)[:maxPrefixLength-3]))
	}
	return prefix
}

func formatAbove(text string) string {
	return fmt.Sprintf("\t\t\t\t%s", text)
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

func getFiles(srcPath string, srcInfo os.FileInfo) ([]string, error) {
	var files []string
	if srcInfo.IsDir() {
		entries, err := os.ReadDir(srcPath)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				files = append(files, path.Join(srcPath, entry.Name()))
			}
		}
	} else {
		files = append(files, srcPath)
	}
	return files, nil
}

func processFile(filePath, outPath string, bar *progressbar.Bar) error {
	bar.WriteAbove(formatAbove("unzipping..."))
	outDir, err := unzipEpub(filePath, outPath)
	if err != nil {
		return err
	}
	defer func() {
		bar.WriteAbove(formatAbove("cleaning..."))
		_ = cleanFiles(outDir)
	}()
	err = processImages(outDir, bar)
	if err != nil {
		return err
	}
	bar.WriteAbove(formatAbove("zipping..."))
	return zipEpub(outDir)
}

func unzipEpub(filePath, outPath string) (string, error) {
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

func processImages(imagePath string, bar *progressbar.Bar) error {
	bar.WriteAbove(formatAbove("scanning..."))
	images := scanImages(imagePath)
	for i, imagePath := range images {
		progress := float64(i+1) / float64(len(images)) * 100
		if int(progress) <= 99 {
			bar.Progress(progress)
		}
		bar.WriteAbove(formatAbove("resizing..."))
		img, err := imaging.Open(imagePath)
		if err != nil {
			return err
		}
		err = os.Remove(imagePath)
		if err != nil {
			return err
		}
		err = imaging.Save(resizeImage(img), imagePath)
		if err != nil {
			return err
		}
	}
	if len(images) == 0 {
		bar.Progress(99)
	}
	return nil
}

func scanImages(basePath string) []string {
	info, err := os.Stat(basePath)
	if err != nil || !info.IsDir() {
		return nil
	}
	var imagePaths []string
	entries, err := os.ReadDir(basePath)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if entry.IsDir() {
			imagePaths = append(imagePaths, scanImages(path.Join(basePath, entry.Name()))...)
			continue
		}
		if strings.HasSuffix(entry.Name(), ".jpg") || strings.HasSuffix(entry.Name(), ".jpeg") || strings.HasSuffix(entry.Name(), ".png") {
			imagePaths = append(imagePaths, path.Join(basePath, entry.Name()))
		}
	}
	return imagePaths
}

func zipEpub(dir string) error {
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
	rootCmd.Flags().IntVarP(&concurrency, "concurrency", "c", 1, "concurrency of process")
}

func main() {
	err := rootCmd.Execute()
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
	os.Exit(0)
}
