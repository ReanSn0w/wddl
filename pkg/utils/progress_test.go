package utils_test

import (
	"testing"

	"git.papkovda.ru/tools/webdav/pkg/utils"
)

func Test_MakeProgressMessage(t *testing.T) {
	testCases := []struct {
		workFiles int
		maxFiles  int
		files     []utils.FileProgress
		expected  string
	}{
		{
			workFiles: 1,
			maxFiles:  3,
			files: []utils.FileProgress{
				utils.NewProgres("file1.txt", 25.0),
			},
			expected: "Downloading: 1 / 3\n1) file1.txt (25.00%)",
		},
		{
			workFiles: 2,
			maxFiles:  2,
			files: []utils.FileProgress{
				utils.NewProgres("file1.txt", 50.0),
				utils.NewProgres("file2.txt", 75.0),
			},
			expected: "Downloading: 2 / 2\n1) file1.txt (50.00%)\n2) file2.txt (75.00%)",
		},
		{
			workFiles: 0,
			maxFiles:  5,
			files:     []utils.FileProgress{},
			expected:  "Downloading: 0 / 5",
		},
	}

	for _, tc := range testCases {
		actual := utils.MakeProgressMessage(tc.workFiles, tc.maxFiles, tc.files...)

		if actual != tc.expected {
			t.Errorf("expected: '%v', got: '%v'", tc.expected, actual)
		}
	}
}
