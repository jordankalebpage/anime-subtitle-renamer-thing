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
	"errors"
	"flag"
	"fmt"
	"io"
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

type RenameOperation struct {
	OldPath string
	NewPath string
}

type AppConfig struct {
	FolderPath string
	AnimeName  string
	DryRun     bool
}

type episodePattern struct {
	regex        *regexp.Regexp
	seasonIndex  int
	episodeIndex int
}

type PreflightError struct {
	Issues []string
}

func (e *PreflightError) Error() string {
	return "preflight checks failed:\n - " + strings.Join(e.Issues, "\n - ")
}

type RenameExecutionError struct {
	Phase string
	From  string
	To    string
	Err   error
}

func (e *RenameExecutionError) Error() string {
	return fmt.Sprintf("rename failed during %s (%s -> %s): %v", e.Phase, e.From, e.To, e.Err)
}

func (e *RenameExecutionError) Unwrap() error {
	return e.Err
}

type renameExecutor func(oldPath string, newPath string) error

type renameState struct {
	RenameOperation
	TempPath    string
	CurrentPath string
}

var stdinReader = bufio.NewReader(os.Stdin)

var episodePatterns = []episodePattern{
	{regex: regexp.MustCompile(`(?i)S(\d+)\s*-\s*(\d+)`), seasonIndex: 1, episodeIndex: 2},
	{regex: regexp.MustCompile(`(?i)S(\d+)(?:\s|E)(\d+)`), seasonIndex: 1, episodeIndex: 2},
	{regex: regexp.MustCompile(`(?i)E(\d+)`), seasonIndex: 0, episodeIndex: 1},
	{regex: regexp.MustCompile(`\s-\s\(?(\d+)\)?`), seasonIndex: 0, episodeIndex: 1},
	{regex: regexp.MustCompile(`\s(\d{2,3})(?:\s|$)`), seasonIndex: 0, episodeIndex: 1},
}

var flexiblePattern = regexp.MustCompile(`\d+`)

var videoExtensions = []string{".mkv", ".mp4", ".avi"}

var subtitleExtensions = []string{".srt", ".ass"}

func main() {
	config, err := loadConfig()
	if err != nil {
		exitWithError(err)
	}

	videoFiles, err := findFiles(config.FolderPath, videoExtensions)
	if err != nil {
		exitWithError(err)
	}

	subtitleFiles, err := findFiles(config.FolderPath, subtitleExtensions)
	if err != nil {
		exitWithError(err)
	}

	if len(videoFiles) == 0 && len(subtitleFiles) == 0 {
		exitWithError(errors.New("no video or subtitle files found"))
	}

	if len(videoFiles) != len(subtitleFiles) {
		fmt.Printf(
			"Warning: found %d video files and %d subtitle files.\n",
			len(videoFiles),
			len(subtitleFiles),
		)
	}

	pairs, unmatched := createFilePairs(videoFiles, subtitleFiles)
	displayPairsAndUnmatched(pairs, unmatched)

	operations := buildRenameOperations(pairs, config.AnimeName)

	if err := preflightRenameOperations(operations); err != nil {
		exitWithError(err)
	}

	if config.DryRun {
		fmt.Println("\nDry-run mode enabled. No files will be changed.")
		if err := executeRenameOperations(operations, true); err != nil {
			exitWithError(err)
		}
		fmt.Println("Dry-run complete.")
		return
	}

	confirmed, err := confirmRename()
	if err != nil {
		exitWithError(err)
	}

	if !confirmed {
		fmt.Println("Renaming cancelled.")
		return
	}

	if err := executeRenameOperations(operations, false); err != nil {
		exitWithError(err)
	}

	fmt.Println("All done :)")
}

func loadConfig() (AppConfig, error) {
	var dryRun bool
	flag.BoolVar(&dryRun, "dry-run", false, "print planned renames without changing files")
	flag.Parse()

	folderPath, err := getUserInputLine("Enter the path to the folder containing the videos and subtitles: ")
	if err != nil {
		return AppConfig{}, fmt.Errorf("reading folder path: %w", err)
	}

	if err := validateFolderPath(folderPath); err != nil {
		return AppConfig{}, err
	}

	animeName, err := getUserInputLine("Enter the name of the anime: ")
	if err != nil {
		return AppConfig{}, fmt.Errorf("reading anime name: %w", err)
	}

	if err := validateAnimeName(animeName); err != nil {
		return AppConfig{}, err
	}

	return AppConfig{
		FolderPath: folderPath,
		AnimeName:  animeName,
		DryRun:     dryRun,
	}, nil
}

func validateFolderPath(folderPath string) error {
	if strings.TrimSpace(folderPath) == "" {
		return errors.New("folder path is empty")
	}

	info, err := os.Stat(folderPath)
	if err != nil {
		return fmt.Errorf("checking folder path: %w", err)
	}

	if !info.IsDir() {
		return fmt.Errorf("folder path is not a directory: %s", folderPath)
	}

	return nil
}

func validateAnimeName(animeName string) error {
	if strings.TrimSpace(animeName) == "" {
		return errors.New("anime name is empty")
	}

	if strings.ContainsAny(animeName, `<>:"/\|?*`) {
		return fmt.Errorf("anime name contains invalid filename characters: %s", animeName)
	}

	return nil
}

func getUserInputLine(prompt string) (string, error) {
	fmt.Print(prompt)
	input, err := stdinReader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}

	trimmedInput := strings.TrimSpace(input)
	if errors.Is(err, io.EOF) && trimmedInput == "" {
		return "", io.EOF
	}

	return trimmedInput, nil
}

func exitWithError(err error) {
	fmt.Printf("Error: %v\n", err)
	os.Exit(1)
}

func findFiles(folderPath string, extensions []string) ([]FileInfo, error) {
	files := []FileInfo{}
	extensionSet := map[string]struct{}{}

	for _, ext := range extensions {
		normalizedExtension := strings.ToLower(ext)
		extensionSet[normalizedExtension] = struct{}{}
	}

	err := filepath.Walk(folderPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("accessing path %q: %w", path, err)
		}

		if info.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if _, exists := extensionSet[ext]; !exists {
			return nil
		}

		baseName := filepath.Base(path)
		if !flexiblePattern.MatchString(baseName) {
			return nil
		}

		season, episode := extractSeasonAndEpisode(baseName)
		if episode == 0 {
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
		return nil, fmt.Errorf("walking folder %q: %w", folderPath, err)
	}

	return files, nil
}

func extractSeasonAndEpisode(filename string) (int, int) {
	filenameWithoutExtension := strings.TrimSuffix(filename, filepath.Ext(filename))

	for _, pattern := range episodePatterns {
		match := pattern.regex.FindStringSubmatch(filenameWithoutExtension)
		if len(match) <= pattern.episodeIndex {
			continue
		}

		episode, err := strconv.Atoi(match[pattern.episodeIndex])
		if err != nil || episode == 0 {
			continue
		}

		season := 1
		if pattern.seasonIndex > 0 {
			parsedSeason, parseErr := strconv.Atoi(match[pattern.seasonIndex])
			if parseErr == nil && parsedSeason > 0 {
				season = parsedSeason
			}
		}

		return season, episode
	}

	return 1, 0
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

func confirmRename() (bool, error) {
	for {
		response, err := getUserInputLine("\nDo you want to proceed with renaming? (yes/no): ")
		if err != nil {
			return false, err
		}

		response = strings.ToLower(strings.TrimSpace(response))

		if response == "yes" || response == "y" {
			return true, nil
		}

		if response == "no" || response == "n" {
			return false, nil
		}

		fmt.Println("Please answer with yes/y or no/n.")
	}
}

func buildRenameOperations(pairs []FilePair, animeName string) []RenameOperation {
	operations := make([]RenameOperation, 0, len(pairs)*2)

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

		operations = append(operations, RenameOperation{
			OldPath: pair.Video.Path,
			NewPath: filepath.Join(filepath.Dir(pair.Video.Path), newVideoName),
		})

		operations = append(operations, RenameOperation{
			OldPath: pair.Subtitle.Path,
			NewPath: filepath.Join(filepath.Dir(pair.Subtitle.Path), newSubtitleName),
		})
	}

	return operations
}

func preflightRenameOperations(operations []RenameOperation) error {
	issues := []string{}

	if len(operations) == 0 {
		issues = append(issues, "no matched file pairs were found")
	}

	sourcePaths := map[string]struct{}{}
	targetPaths := map[string]struct{}{}

	for _, operation := range operations {
		if strings.TrimSpace(operation.OldPath) == "" {
			issues = append(issues, "operation contains empty source path")
			continue
		}

		if strings.TrimSpace(operation.NewPath) == "" {
			issues = append(issues, fmt.Sprintf("operation for %s contains empty target path", operation.OldPath))
			continue
		}

		sourcePaths[operation.OldPath] = struct{}{}

		if _, err := os.Stat(operation.OldPath); err != nil {
			issues = append(issues, fmt.Sprintf("source file does not exist or is not readable: %s", operation.OldPath))
			continue
		}

		if operation.OldPath == operation.NewPath {
			continue
		}

		if _, exists := targetPaths[operation.NewPath]; exists {
			issues = append(issues, fmt.Sprintf("duplicate target path detected: %s", operation.NewPath))
			continue
		}

		targetPaths[operation.NewPath] = struct{}{}
	}

	for targetPath := range targetPaths {
		if _, exists := sourcePaths[targetPath]; exists {
			continue
		}

		_, statErr := os.Stat(targetPath)
		if statErr == nil {
			issues = append(issues, fmt.Sprintf("target path already exists: %s", targetPath))
			continue
		}

		if !errors.Is(statErr, os.ErrNotExist) {
			issues = append(issues, fmt.Sprintf("unable to validate target path %s: %v", targetPath, statErr))
		}
	}

	if len(issues) > 0 {
		return &PreflightError{Issues: issues}
	}

	return nil
}

func executeRenameOperations(operations []RenameOperation, dryRun bool) error {
	return executeRenameOperationsWith(operations, dryRun, os.Rename)
}

func executeRenameOperationsWith(
	operations []RenameOperation,
	dryRun bool,
	renameFn renameExecutor,
) error {
	if dryRun {
		for _, operation := range operations {
			if operation.OldPath == operation.NewPath {
				fmt.Printf("[dry-run] No change: %s\n", operation.OldPath)
				continue
			}

			fmt.Printf("[dry-run] %s -> %s\n", operation.OldPath, operation.NewPath)
		}

		return nil
	}

	states := make([]renameState, 0, len(operations))

	for index, operation := range operations {
		if operation.OldPath == operation.NewPath {
			fmt.Printf("No change: %s\n", operation.OldPath)
			continue
		}

		tempPath, err := buildTempPath(operation.OldPath, index)
		if err != nil {
			return err
		}

		states = append(states, renameState{
			RenameOperation: operation,
			TempPath:        tempPath,
			CurrentPath:     operation.OldPath,
		})
	}

	if len(states) == 0 {
		fmt.Println("No files need renaming.")
		return nil
	}

	for index := range states {
		state := &states[index]
		if err := renameFn(state.CurrentPath, state.TempPath); err != nil {
			executionErr := &RenameExecutionError{
				Phase: "phase-one",
				From:  state.CurrentPath,
				To:    state.TempPath,
				Err:   err,
			}

			rollbackErr := rollbackRenameStates(states, renameFn)
			if rollbackErr != nil {
				return errors.Join(executionErr, fmt.Errorf("rollback failed: %w", rollbackErr))
			}

			return executionErr
		}

		state.CurrentPath = state.TempPath
	}

	for index := range states {
		state := &states[index]
		if err := renameFn(state.CurrentPath, state.NewPath); err != nil {
			executionErr := &RenameExecutionError{
				Phase: "phase-two",
				From:  state.CurrentPath,
				To:    state.NewPath,
				Err:   err,
			}

			rollbackErr := rollbackRenameStates(states, renameFn)
			if rollbackErr != nil {
				return errors.Join(executionErr, fmt.Errorf("rollback failed: %w", rollbackErr))
			}

			return executionErr
		}

		state.CurrentPath = state.NewPath
	}

	for _, state := range states {
		fmt.Printf("Renamed: %s -> %s\n", state.OldPath, state.NewPath)
	}

	return nil
}

func buildTempPath(oldPath string, index int) (string, error) {
	dir := filepath.Dir(oldPath)
	base := filepath.Base(oldPath)

	for attempt := range 1000 {
		candidate := filepath.Join(
			dir,
			fmt.Sprintf(".anime-renamer-tmp-%d-%d-%s", os.Getpid(), index*1000+attempt, base),
		)

		_, err := os.Stat(candidate)
		if errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		}

		if err != nil {
			return "", fmt.Errorf("checking temp path %s: %w", candidate, err)
		}
	}

	return "", fmt.Errorf("failed to allocate temp path for %s", oldPath)
}

func rollbackRenameStates(states []renameState, renameFn renameExecutor) error {
	rollbackErrors := []error{}

	for index := len(states) - 1; index >= 0; index-- {
		state := states[index]
		if state.CurrentPath == state.OldPath {
			continue
		}

		_, statErr := os.Stat(state.CurrentPath)
		if statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				rollbackErrors = append(
					rollbackErrors,
					fmt.Errorf("rollback source disappeared: %s", state.CurrentPath),
				)
				continue
			}

			rollbackErrors = append(
				rollbackErrors,
				fmt.Errorf("rollback stat failed for %s: %w", state.CurrentPath, statErr),
			)
			continue
		}

		if err := renameFn(state.CurrentPath, state.OldPath); err != nil {
			rollbackErrors = append(
				rollbackErrors,
				fmt.Errorf("rollback failed (%s -> %s): %w", state.CurrentPath, state.OldPath, err),
			)
		}
	}

	if len(rollbackErrors) > 0 {
		return errors.Join(rollbackErrors...)
	}

	return nil
}
