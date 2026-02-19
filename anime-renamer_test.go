package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractSeasonAndEpisode(t *testing.T) {
	testCases := []struct {
		name        string
		filename    string
		wantSeason  int
		wantEpisode int
	}{
		{
			name:        "S and episode with dash",
			filename:    "Show S2 - 03.mkv",
			wantSeason:  2,
			wantEpisode: 3,
		},
		{
			name:        "S and E compact",
			filename:    "Show S01E12.ass",
			wantSeason:  1,
			wantEpisode: 12,
		},
		{
			name:        "episode only with E prefix",
			filename:    "Show E09.mp4",
			wantSeason:  1,
			wantEpisode: 9,
		},
		{
			name:        "trailing numeric episode",
			filename:    "Show 021.srt",
			wantSeason:  1,
			wantEpisode: 21,
		},
		{
			name:        "no episode",
			filename:    "Show Finale.mkv",
			wantSeason:  1,
			wantEpisode: 0,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			gotSeason, gotEpisode := extractSeasonAndEpisode(testCase.filename)
			if gotSeason != testCase.wantSeason || gotEpisode != testCase.wantEpisode {
				t.Fatalf(
					"extractSeasonAndEpisode(%q) = (%d, %d), want (%d, %d)",
					testCase.filename,
					gotSeason,
					gotEpisode,
					testCase.wantSeason,
					testCase.wantEpisode,
				)
			}
		})
	}
}

func TestPreflightRenameOperationsDetectsDuplicateTargets(t *testing.T) {
	tempDir := t.TempDir()

	sourceOne := filepath.Join(tempDir, "episode-01.mkv")
	sourceTwo := filepath.Join(tempDir, "episode-01.srt")
	duplicateTarget := filepath.Join(tempDir, "Anime - S01E01.mkv")

	if err := os.WriteFile(sourceOne, []byte("video"), 0o600); err != nil {
		t.Fatalf("create source one: %v", err)
	}

	if err := os.WriteFile(sourceTwo, []byte("subtitle"), 0o600); err != nil {
		t.Fatalf("create source two: %v", err)
	}

	operations := []RenameOperation{
		{OldPath: sourceOne, NewPath: duplicateTarget},
		{OldPath: sourceTwo, NewPath: duplicateTarget},
	}

	err := preflightRenameOperations(operations)
	if err == nil {
		t.Fatal("expected preflight error, got nil")
	}

	if !strings.Contains(err.Error(), "duplicate target path detected") {
		t.Fatalf("expected duplicate target message, got: %v", err)
	}
}

func TestExecuteRenameOperationsWithDryRunDoesNotRename(t *testing.T) {
	tempDir := t.TempDir()

	oldPath := filepath.Join(tempDir, "episode-01.mkv")
	newPath := filepath.Join(tempDir, "Anime - S01E01.mkv")

	if err := os.WriteFile(oldPath, []byte("video"), 0o600); err != nil {
		t.Fatalf("create source file: %v", err)
	}

	renameCalled := false
	renameFn := func(oldPath string, newPath string) error {
		renameCalled = true
		return os.Rename(oldPath, newPath)
	}

	err := executeRenameOperationsWith(
		[]RenameOperation{{OldPath: oldPath, NewPath: newPath}},
		true,
		renameFn,
	)
	if err != nil {
		t.Fatalf("execute dry-run: %v", err)
	}

	if renameCalled {
		t.Fatal("expected rename function to not be called in dry-run mode")
	}

	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("expected original file to remain: %v", err)
	}

	if _, err := os.Stat(newPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected target file to not exist, got: %v", err)
	}
}

func TestExecuteRenameOperationsWithRollback(t *testing.T) {
	tempDir := t.TempDir()

	oldVideo := filepath.Join(tempDir, "episode-01.mkv")
	oldSubtitle := filepath.Join(tempDir, "episode-01.srt")
	newVideo := filepath.Join(tempDir, "Anime - S01E01.mkv")
	newSubtitle := filepath.Join(tempDir, "Anime - S01E01.srt")

	if err := os.WriteFile(oldVideo, []byte("video"), 0o600); err != nil {
		t.Fatalf("create video file: %v", err)
	}

	if err := os.WriteFile(oldSubtitle, []byte("subtitle"), 0o600); err != nil {
		t.Fatalf("create subtitle file: %v", err)
	}

	renameFn := func(oldPath string, newPath string) error {
		if newPath == newSubtitle {
			return errors.New("forced failure for rollback test")
		}

		return os.Rename(oldPath, newPath)
	}

	err := executeRenameOperationsWith(
		[]RenameOperation{
			{OldPath: oldVideo, NewPath: newVideo},
			{OldPath: oldSubtitle, NewPath: newSubtitle},
		},
		false,
		renameFn,
	)
	if err == nil {
		t.Fatal("expected execution error, got nil")
	}

	if _, statErr := os.Stat(oldVideo); statErr != nil {
		t.Fatalf("expected video file restored to original name: %v", statErr)
	}

	if _, statErr := os.Stat(oldSubtitle); statErr != nil {
		t.Fatalf("expected subtitle file restored to original name: %v", statErr)
	}

	if _, statErr := os.Stat(newVideo); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected renamed video to not exist after rollback, got: %v", statErr)
	}

	if _, statErr := os.Stat(newSubtitle); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected renamed subtitle to not exist after rollback, got: %v", statErr)
	}
}
