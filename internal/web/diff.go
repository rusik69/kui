package web

import "strings"

// DiffLine is one line of a line-based diff.
type DiffLine struct {
	Type string // "eq" | "add" | "del"
	Text string
}

// DiffLines computes a line-based diff of a vs b using a classic LCS
// backtrack. Inputs here are single Kubernetes manifests (a few hundred
// lines at most), so the O(len(a)*len(b)) table is comfortably fast —
// no external diff dependency needed at this scale.
func DiffLines(a, b string) []DiffLine {
	as := splitLines(a)
	bs := splitLines(b)
	n, m := len(as), len(bs)

	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if as[i] == bs[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	var out []DiffLine
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case as[i] == bs[j]:
			out = append(out, DiffLine{Type: "eq", Text: as[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			out = append(out, DiffLine{Type: "del", Text: as[i]})
			i++
		default:
			out = append(out, DiffLine{Type: "add", Text: bs[j]})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, DiffLine{Type: "del", Text: as[i]})
	}
	for ; j < m; j++ {
		out = append(out, DiffLine{Type: "add", Text: bs[j]})
	}
	return out
}

func splitLines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
