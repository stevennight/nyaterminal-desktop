package version

import (
	"fmt"
	"strconv"
	"strings"
)

var (
	Version          = "0.1.0-dev"
	Commit           = ""
	BuildDate        = ""
	UpdateRepository = "nyaterminal/nyaterminal-desktop"
)

type BuildInfo struct {
	Version          string `json:"version"`
	Commit           string `json:"commit,omitempty"`
	BuildDate        string `json:"buildDate,omitempty"`
	UpdateRepository string `json:"updateRepository,omitempty"`
}

func Info() BuildInfo {
	return BuildInfo{
		Version:          Version,
		Commit:           Commit,
		BuildDate:        BuildDate,
		UpdateRepository: strings.TrimSpace(UpdateRepository),
	}
}

func String() string {
	parts := []string{Version}
	if strings.TrimSpace(Commit) != "" {
		parts = append(parts, "commit="+Commit)
	}
	if strings.TrimSpace(BuildDate) != "" {
		parts = append(parts, "built="+BuildDate)
	}
	if strings.TrimSpace(UpdateRepository) != "" {
		parts = append(parts, "updates="+strings.TrimSpace(UpdateRepository))
	}
	return strings.Join(parts, " ")
}

func Print(name string) string {
	return fmt.Sprintf("%s %s", name, String())
}

func IsVersionCommand(args []string) bool {
	return len(args) == 1 && (args[0] == "--version" || args[0] == "-version" || args[0] == "version")
}

func Compare(a, b string) int {
	aa := parts(a)
	bb := parts(b)
	for i := 0; i < len(aa) || i < len(bb); i++ {
		var av, bv int
		if i < len(aa) {
			av = aa[i]
		}
		if i < len(bb) {
			bv = bb[i]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func parts(value string) []int {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "desktop-")
	value = strings.TrimPrefix(value, "server-")
	value = strings.TrimPrefix(value, "v")
	if idx := strings.IndexAny(value, "+-"); idx >= 0 {
		value = value[:idx]
	}
	raw := strings.Split(value, ".")
	out := make([]int, 0, len(raw))
	for _, part := range raw {
		n, _ := strconv.Atoi(part)
		out = append(out, n)
	}
	return out
}
