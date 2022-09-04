package txtindex

import (
	"archive/tar"
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	s "strings"

	"github.com/wizzomafizzo/mrext/pkg/config"
	"github.com/wizzomafizzo/mrext/pkg/utils"
)

var indexFilename = config.INDEX_NAME + ".tar"

func GetIndexPath() string {
	if _, err := os.Stat(config.SD_ROOT); err == nil {
		return filepath.Join(config.SD_ROOT, indexFilename)
	} else {
		return indexFilename
	}
}

func Exists() bool {
	_, err := os.Stat(GetIndexPath())
	return err == nil
}

func Generate(files [][2]string, indexPath string) error {
	tmpDir, err := os.MkdirTemp(os.TempDir(), "search-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	tmpFilesDir := filepath.Join(tmpDir, "files")
	if err := os.Mkdir(tmpFilesDir, 0755); err != nil {
		return err
	}

	indexFiles := make(map[string]*os.File)
	getIndexFile := func(fn string) (*os.File, error) {
		if _, ok := indexFiles[fn]; !ok {
			indexFiles[fn], err = os.Create(filepath.Join(tmpFilesDir, fn))
			if err != nil {
				return nil, err
			}
		}

		return indexFiles[fn], nil
	}

	for i := range files {
		pathsFile, err := getIndexFile(files[i][0] + "__paths")
		if err != nil {
			return err
		}

		namesFile, err := getIndexFile(files[i][0] + "__names")
		if err != nil {
			return err
		}

		basename := filepath.Base(files[i][1])
		name := s.TrimSuffix(basename, filepath.Ext(basename))

		pathsFile.WriteString(files[i][1] + "\n")
		namesFile.WriteString(name + "\n")
	}

	for _, f := range indexFiles {
		f.Sync()
		f.Close()
	}

	tmpIndexPath := filepath.Join(tmpDir, indexFilename)

	indexTar, err := os.Create(tmpIndexPath)
	if err != nil {
		return err
	}

	tarw := tar.NewWriter(indexTar)
	defer tarw.Close()

	tmpFiles, err := os.ReadDir(tmpFilesDir)
	if err != nil {
		return err
	}

	for _, indexFile := range tmpFiles {
		file, err := os.Open(filepath.Join(tmpFilesDir, indexFile.Name()))
		if err != nil {
			return err
		}
		defer file.Close()

		fileInfo, err := indexFile.Info()
		if err != nil {
			return err
		}

		header := &tar.Header{
			Name:    indexFile.Name(),
			Size:    fileInfo.Size(),
			Mode:    int64(fileInfo.Mode()),
			ModTime: fileInfo.ModTime(),
		}

		err = tarw.WriteHeader(header)
		if err != nil {
			return err
		}

		if _, err := io.Copy(tarw, file); err != nil {
			return err
		}
	}

	utils.MoveFile(tmpIndexPath, indexPath)

	return nil
}

type indexMap map[string]map[string][]string

type Index struct {
	Path  string
	files indexMap
}

func Open(indexPath string) (Index, error) {
	var index Index

	_, err := os.Stat(indexPath)
	if err != nil {
		return index, err
	}

	indexTar, err := os.Open(indexPath)
	if err != nil {
		return index, err
	}
	defer indexTar.Close()

	index.Path = indexPath
	index.files = make(map[string]map[string][]string)

	r := tar.NewReader(indexTar)
	for {
		header, err := r.Next()

		if err == io.EOF {
			break
		} else if err != nil {
			return index, err
		}

		if header.Typeflag == tar.TypeReg {
			bs := bufio.NewScanner(r)
			lines := make([]string, 0)

			for bs.Scan() {
				lines = append(lines, bs.Text())
			}

			if err := bs.Err(); err != nil {
				return index, err
			}

			hp := s.Split(header.Name, "__")

			if len(hp) != 2 {
				return index, fmt.Errorf("invalid index file: %s", header.Name)
			}

			if _, ok := index.files[hp[0]]; !ok {
				index.files[hp[0]] = make(map[string][]string)
			}

			index.files[hp[0]][hp[1]] = lines
		}
	}

	return index, nil
}

type SearchResult struct {
	System string
	Name   string
	Path   string
}

func (idx *Index) searchSystemNameGeneric(test func(string, string) bool, system string, query string) []SearchResult {
	var results []SearchResult
	for i, name := range idx.files[system]["names"] {
		if test(name, query) {
			results = append(results, SearchResult{
				System: system,
				Name:   name,
				Path:   idx.files[system]["paths"][i],
			})
		}
	}
	return results
}

func searchNameTest(name string, query string) bool {
	return s.Contains(s.ToLower(name), query)
}

func (idx *Index) SearchAllName(query string) []SearchResult {
	var results []SearchResult
	query = s.ToLower(query)
	for system := range idx.files {
		results = append(results, idx.searchSystemNameGeneric(searchNameTest, system, query)...)
	}
	return results
}

func (idx *Index) SearchSystemName(system string, query string) []SearchResult {
	query = s.ToLower(query)
	return idx.searchSystemNameGeneric(searchNameTest, system, query)
}

func searchNameReTest(name string, query string) bool {
	re, err := regexp.Compile(query)
	if err != nil {
		return false
	}
	return re.MatchString(name)
}

func (idx *Index) SearchSystemNameRe(system string, query string) []SearchResult {
	return idx.searchSystemNameGeneric(searchNameReTest, system, query)
}

func (idx *Index) Total() int {
	total := 0
	for system := range idx.files {
		total += len(idx.files[system]["paths"])
	}
	return total
}