/*
	anime-renamer.go

The intention of this program is to rename anime videos and
subtitle files so mpv can find the subtitles and auto load them.

It assumes the videos and subtitles are in the same folder.

Possible video formats: .mkv, .mp4, .avi

Possible subtitle formats: .srt, .ass

The program will try to find the episode number in the following order:

1. S1 - 01

2. S1E01

3. E01

4. - 01

5. 01 or 001 at the end or before space

If season number isn't found in either the video or subtitle file name,
it will normalize to only use episode number.
e.g., if season 1 has 12 episodes, and season 2 has 12 episodes,
then the file names will be E1..E24

If season number *is* found in both the video and subtitle file name,
then season number will be retained.
*/
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type FileInfo struct {
	Path      string
	Season    int
	Episode   int
	Extension string
}

type FilePair struct {
	Video    FileInfo
	Subtitle FileInfo
}

func main() {
	folderPath := getUserInputLine(
		"Enter the path to the folder containing the videos and subtitles:",
	)
	if folderPath == "" {
		exitWithError("Error: Folder path is empty")
	}

	fmt.Printf("Debug: Folder path entered: %s\n", folderPath)

	animeName := getUserInputLine("Enter the name of the anime:")

	// Use a default flexible naming convention
	namingConvention := "S#E#"

	fmt.Printf("Debug: Using flexible naming convention: %s\n", namingConvention)

	videoFiles := findFiles(folderPath, []string{".mkv", ".mp4", ".avi"})
	subtitleFiles := findFiles(folderPath, []string{".srt", ".ass"})

	fmt.Printf(
		"Debug: Found %d video files and %d subtitle files\n",
		len(videoFiles),
		len(subtitleFiles),
	)

	if len(videoFiles) == 0 && len(subtitleFiles) == 0 {
		exitWithError(
			"Error: No video or subtitle files found. Please check the folder path and naming conventions.",
		)
	}

	if len(videoFiles) != len(subtitleFiles) {
		fmt.Println("Warning: Number of video files does not match number of subtitle files")
		fmt.Printf(
			"Found %d video files and %d subtitle files\n",
			len(videoFiles),
			len(subtitleFiles),
		)
		fmt.Println("Press enter to continue or ctrl+c to exit...")
		fmt.Scanln()
	}

	pairs, unmatched := createFilePairs(videoFiles, subtitleFiles)
	displayPairsAndUnmatched(pairs, unmatched)

	if confirmRename() {
		renamePairs(pairs, animeName)
	} else {
		fmt.Println("Renaming cancelled.")
	}

	fmt.Println("All done :) またねー！")
	fmt.Println("Press enter to exit...")
	fmt.Scanln()
}

func getUserInputLine(prompt string) string {
	reader := bufio.NewReader(os.Stdin)
	fmt.Println(prompt)
	input, _ := reader.ReadString('\n')

	return strings.TrimSpace(input)
}

func exitWithError(message string) {
	fmt.Println(message)
	fmt.Println("Press enter to exit...")
	fmt.Scanln()

	os.Exit(1)
}

func findFiles(folderPath string, extensions []string) []FileInfo {
	var files []FileInfo
	extensionSet := make(map[string]bool)

	for _, ext := range extensions {
		extensionSet[ext] = true
	}

	pattern := createFlexiblePattern()

	err := filepath.Walk(folderPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			fmt.Printf("Error accessing path %q: %v\n", path, err)
			return nil
		}

		if info.IsDir() {
			return nil
		}

		ext := filepath.Ext(path)
		if !extensionSet[ext] {
			return nil
		}

		baseName := filepath.Base(path)
		if !pattern.MatchString(baseName) {
			fmt.Printf("Debug: File not matched: %s\n", baseName)
			return nil
		}

		fmt.Printf("Debug: Matched file: %s\n", baseName)
		season, episode := extractSeasonAndEpisode(baseName)
		fmt.Printf("Debug: Extracted Season: %d, Episode: %d\n", season, episode)

		if episode == 0 {
			fmt.Printf("Debug: Skipped file (no valid episode number): %s\n", baseName)
			return nil
		}

		files = append(files, FileInfo{
			Path:      path,
			Season:    season,
			Episode:   episode,
			Extension: ext,
		})

		return nil
	})

	if err != nil {
		fmt.Printf("Error walking the path %q: %v\n", folderPath, err)
	}

	return files
}

func extractSeasonAndEpisode(filename string) (int, int) {
	seasonStr := ""
	episodeStr := ""

	// Array of patterns to try
	patterns := []struct {
		regex                     *regexp.Regexp
		seasonIndex, episodeIndex int
	}{
		{regexp.MustCompile(`S(\d+)\s*-\s*(\d+)`), 1, 2}, // S1 - 01
		{
			regexp.MustCompile(`S(\d+)(?:\s|E)(\d+)`),
			1,
			2,
		}, // S1E01 or S01E01 or S1 01 etc
		{regexp.MustCompile(`E(\d+)`), 0, 1},              // E01
		{regexp.MustCompile(`\s-\s\(?(\d+)\)?`), 0, 1},    // - 01 or - (01)
		{regexp.MustCompile(`\s(\d{2,3})(?:\s|$)`), 0, 1}, // 01 or 001 at the end or before space
	}

	for _, pattern := range patterns {
		if match := pattern.regex.FindStringSubmatch(filename); len(match) > pattern.episodeIndex {
			if pattern.seasonIndex > 0 {
				seasonStr = match[pattern.seasonIndex]
			}

			episodeStr = match[pattern.episodeIndex]
			break
		}
	}

	season, _ := strconv.Atoi(seasonStr)
	episode, _ := strconv.Atoi(episodeStr)

	// If no season found, default to 1
	if season == 0 {
		season = 1
	}

	return season, episode
}

func createFlexiblePattern() *regexp.Regexp {
	return regexp.MustCompile(`\d+`)
}

func createFilePairs(videoFiles, subtitleFiles []FileInfo) ([]FilePair, []FileInfo) {
	pairs := []FilePair{}
	unmatched := []FileInfo{}
	subtitleMap := make(map[int]FileInfo)

	for _, subtitle := range subtitleFiles {
		key := subtitle.Season*1000 + subtitle.Episode
		subtitleMap[key] = subtitle
	}

	for _, video := range videoFiles {
		key := video.Season*1000 + video.Episode

		if subtitle, exists := subtitleMap[key]; exists {
			pairs = append(pairs, FilePair{Video: video, Subtitle: subtitle})
			delete(subtitleMap, key)
		} else {
			unmatched = append(unmatched, video)
		}
	}

	for _, subtitle := range subtitleMap {
		unmatched = append(unmatched, subtitle)
	}

	return pairs, unmatched
}

func displayPairsAndUnmatched(pairs []FilePair, unmatched []FileInfo) {
	fmt.Println("\nMatched pairs:")

	for i, pair := range pairs {
		fmt.Printf(
			"%d. Video: %s\n   Subtitle: %s\n",
			i+1,
			filepath.Base(pair.Video.Path),
			filepath.Base(pair.Subtitle.Path),
		)
	}

	if len(unmatched) > 0 {
		fmt.Println("\nUnmatched files:")

		for i, file := range unmatched {
			fmt.Printf("%d. %s\n", i+1, filepath.Base(file.Path))
		}
	}
}

func confirmRename() bool {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("\nDo you want to proceed with renaming? (yes/no): ")
		response, _ := reader.ReadString('\n')
		response = strings.ToLower(strings.TrimSpace(response))

		if response == "yes" || response == "y" {
			return true
		} else if response == "no" || response == "n" {
			return false
		}

		fmt.Println("Please answer 'yes' or 'no'.")
	}
}

func renamePairs(pairs []FilePair, animeName string) {
	for _, pair := range pairs {
		newVideoName := fmt.Sprintf(
			"%s - S%02dE%02d%s",
			animeName,
			pair.Video.Season,
			pair.Video.Episode,
			pair.Video.Extension,
		)

		newSubtitleName := fmt.Sprintf(
			"%s - S%02dE%02d%s",
			animeName,
			pair.Subtitle.Season,
			pair.Subtitle.Episode,
			pair.Subtitle.Extension,
		)

		renameFile(pair.Video.Path, filepath.Join(filepath.Dir(pair.Video.Path), newVideoName))
		renameFile(
			pair.Subtitle.Path,
			filepath.Join(filepath.Dir(pair.Subtitle.Path), newSubtitleName),
		)
	}
}

func renameFile(oldPath, newPath string) {
	err := os.Rename(oldPath, newPath)

	if err != nil {
		fmt.Printf("Error renaming file %s to %s: %v\n", oldPath, newPath, err)
	} else {
		fmt.Printf("Renamed: %s -> %s\n", oldPath, newPath)
	}
}
