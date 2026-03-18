package main

import (
	"testing"
)

func TestResolveOutputPath(t *testing.T) {
	tests := []struct {
		name           string
		outputPath     string
		fileOutputPath string
		want           string
	}{
		{
			name:           "no file-output leaves path unchanged",
			outputPath:     "./out",
			fileOutputPath: "",
			want:           "./out",
		},
		{
			name:           "absolute output path unchanged",
			outputPath:     "/tmp/oci-out",
			fileOutputPath: "/tmp/build/result.json",
			want:           "/tmp/oci-out",
		},
		{
			name:           "same directory",
			outputPath:     "out",
			fileOutputPath: "result.json",
			want:           "out",
		},
		{
			name:           "file-output in subdirectory",
			outputPath:     "out",
			fileOutputPath: "build/result.json",
			want:           "../out",
		},
		{
			name:           "output in subdirectory of file-output dir",
			outputPath:     "build/out",
			fileOutputPath: "build/result.json",
			want:           "out",
		},
		{
			name:           "both in different subdirectories",
			outputPath:     "dist/oci",
			fileOutputPath: "build/result.json",
			want:           "../dist/oci",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveOutputPath(tt.outputPath, tt.fileOutputPath)
			if got != tt.want {
				t.Errorf("resolveOutputPath(%q, %q) = %q, want %q", tt.outputPath, tt.fileOutputPath, got, tt.want)
			}
		})
	}
}
