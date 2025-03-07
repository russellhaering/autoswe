package index

import (
	"reflect"
	"testing"
)

func TestMergeRanges(t *testing.T) {
	tests := []struct {
		name     string
		ranges   []snippetRange
		expected []snippetRange
	}{
		{
			name:     "empty input",
			ranges:   nil,
			expected: nil,
		},
		{
			name: "single range",
			ranges: []snippetRange{
				{startLine: 10, endLine: 20},
			},
			expected: []snippetRange{
				{startLine: 10, endLine: 20},
			},
		},
		{
			name: "non-overlapping ranges far apart",
			ranges: []snippetRange{
				{startLine: 10, endLine: 20},
				{startLine: 40, endLine: 50},
			},
			expected: []snippetRange{
				{startLine: 10, endLine: 20},
				{startLine: 40, endLine: 50},
			},
		},
		{
			name: "overlapping ranges",
			ranges: []snippetRange{
				{startLine: 10, endLine: 25},
				{startLine: 20, endLine: 30},
			},
			expected: []snippetRange{
				{startLine: 10, endLine: 30},
			},
		},
		{
			name: "ranges within mergeThreshold",
			ranges: []snippetRange{
				{startLine: 10, endLine: 20},
				{startLine: 25, endLine: 35}, // Within 10 lines of previous range
			},
			expected: []snippetRange{
				{startLine: 10, endLine: 35},
			},
		},
		{
			name: "multiple ranges forming chain",
			ranges: []snippetRange{
				{startLine: 10, endLine: 20},
				{startLine: 25, endLine: 35},
				{startLine: 40, endLine: 50},
			},
			expected: []snippetRange{
				{startLine: 10, endLine: 50},
			},
		},
		{
			name: "mixed scenarios",
			ranges: []snippetRange{
				{startLine: 10, endLine: 20},
				{startLine: 25, endLine: 35}, // Within threshold of first
				{startLine: 60, endLine: 70}, // Far from second
				{startLine: 75, endLine: 85}, // Within threshold of third
			},
			expected: []snippetRange{
				{startLine: 10, endLine: 35},
				{startLine: 60, endLine: 85},
			},
		},
		{
			name: "unsorted input",
			ranges: []snippetRange{
				{startLine: 40, endLine: 50},
				{startLine: 10, endLine: 20},
				{startLine: 25, endLine: 35},
			},
			expected: []snippetRange{
				{startLine: 10, endLine: 50},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeRanges(tt.ranges)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("mergeRanges() = %v, want %v", got, tt.expected)
			}
		})
	}
}
