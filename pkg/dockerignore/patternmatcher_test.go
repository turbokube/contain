package dockerignore_test

import (
	"testing"

	"github.com/moby/patternmatcher"
)

func TestMatch(t *testing.T) {

	matchers := make(map[string]*patternmatcher.PatternMatcher)

	ignore := func(n string, path string) {
		match, err := matchers[n].MatchesOrParentMatches(path)
		if err != nil {
			t.Errorf("Match failed %s: %s %v", n, path, err)
		}
		if !match {
			t.Errorf("Should ignore %s: %s", n, path)
		}
	}
	include := func(n string, path string) {
		match, err := matchers[n].MatchesOrParentMatches(path)
		if err != nil {
			t.Errorf("Match failed %s: %s %v", n, path, err)
		}
		if match {
			t.Errorf("Shouldn't ignore %s: %s", n, path)
		}
	}

	matchers["in"], _ = patternmatcher.New([]string{
		"*",
		"!bar*",
	})
	ignore("in", "foo.txt")
	include("in", "bar.txt")

	matchers["all"], _ = patternmatcher.New([]string{})
	include("all", "foo.txt")

}
