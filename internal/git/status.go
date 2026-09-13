package git

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

type Section int

const (
	Staged Section = iota
	Unstaged
	Untracked
	Conflicted
)

var SectionNames = [4]string{"Staged", "Unstaged", "Untracked", "Conflicts"}

type File struct{ Path, OriginalPath, XY, Submodule string }
type Status struct {
	Branch, OID, Upstream string
	Ahead, Behind         int
	Groups                [4][]File
}
type Repository struct{ Root string }

func Discover(ctx context.Context, path string) (Repository, error) {
	r, err := run(ctx, path, "rev-parse", "--show-toplevel")
	if err != nil {
		return Repository{}, err
	}
	return Repository{Root: strings.TrimSuffix(r.Stdout, "\n")}, nil
}
func (r Repository) RepositoryStatus(ctx context.Context) (Status, error) {
	res, err := run(ctx, r.Root, "status", "--porcelain=v2", "--branch", "-z", "--untracked-files=all")
	if err != nil {
		return Status{}, err
	}
	if res.Truncated {
		return Status{}, fmt.Errorf("repository status exceeds 4 MiB; refusing to show an incomplete file list")
	}
	return ParseStatus(res.Stdout)
}

// ParseStatus preserves filenames verbatim, including tabs, newlines and spaces.
func ParseStatus(raw string) (Status, error) {
	var s Status
	records := strings.Split(raw, "\x00")
	for i := 0; i < len(records); i++ {
		line := records[i]
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "# ") {
			key, value, ok := strings.Cut(line[2:], " ")
			if !ok {
				return s, fmt.Errorf("malformed status header")
			}
			switch key {
			case "branch.head":
				s.Branch = value
			case "branch.oid":
				s.OID = value
			case "branch.upstream":
				s.Upstream = value
			case "branch.ab":
				ab := strings.Fields(value)
				if len(ab) != 2 {
					return s, fmt.Errorf("malformed ahead/behind header")
				}
				var err error
				s.Ahead, err = strconv.Atoi(strings.TrimPrefix(ab[0], "+"))
				if err != nil {
					return s, err
				}
				s.Behind, err = strconv.Atoi(strings.TrimPrefix(ab[1], "-"))
				if err != nil {
					return s, err
				}
			}
			continue
		}
		if strings.HasPrefix(line, "? ") {
			s.Groups[Untracked] = append(s.Groups[Untracked], File{Path: line[2:], XY: "??"})
			continue
		}
		if strings.HasPrefix(line, "! ") {
			continue
		}
		n := 9
		switch line[0] {
		case '1':
		case '2':
			n = 10
		case 'u':
			n = 11
		default:
			return s, fmt.Errorf("unknown status record %q", line[0])
		}
		p := strings.SplitN(line, " ", n)
		if len(p) != n || len(p[1]) != 2 {
			return s, fmt.Errorf("malformed status record")
		}
		f := File{Path: p[n-1], XY: p[1], Submodule: p[2]}
		if line[0] == '2' {
			i++
			if i >= len(records) || records[i] == "" {
				return s, fmt.Errorf("missing rename source")
			}
			f.OriginalPath = records[i]
		}
		if line[0] == 'u' {
			s.Groups[Conflicted] = append(s.Groups[Conflicted], f)
			continue
		}
		if f.XY[0] != '.' {
			s.Groups[Staged] = append(s.Groups[Staged], f)
		}
		if f.XY[1] != '.' {
			s.Groups[Unstaged] = append(s.Groups[Unstaged], f)
		}
	}
	return s, nil
}
