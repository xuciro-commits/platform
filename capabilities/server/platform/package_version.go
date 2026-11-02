package platform

import (
	"cmp"
	"fmt"
	"strconv"
	"strings"
)

type PackageVersion struct {
	Major, Minor, Patch int
	Prerelease          string
}

func ParsePackageVersion(value string) (PackageVersion, error) {
	var version PackageVersion
	number, metadata, build := strings.Cut(value, "+")
	var prerelease bool
	number, version.Prerelease, prerelease = strings.Cut(number, "-")
	for _, suffix := range []struct {
		value            string
		present, numeric bool
	}{{version.Prerelease, prerelease, true}, {metadata, build, false}} {
		if !suffix.present {
			continue
		}
		for _, identifier := range strings.Split(suffix.value, ".") {
			if identifier == "" {
				return version, fmt.Errorf("invalid product version %q", value)
			}
			digits := true
			for _, char := range identifier {
				if char < '0' || char > '9' {
					digits = false
				}
				if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char == '-') {
					return version, fmt.Errorf("invalid product version %q", value)
				}
			}
			if suffix.numeric && digits && len(identifier) > 1 && identifier[0] == '0' {
				return version, fmt.Errorf("invalid product version %q", value)
			}
		}
	}
	parts := strings.Split(number, ".")
	if len(parts) != 3 {
		return version, fmt.Errorf("product version %q must be major.minor.patch", value)
	}
	target := []*int{&version.Major, &version.Minor, &version.Patch}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || strconv.Itoa(n) != part {
			return version, fmt.Errorf("invalid product version %q", value)
		}
		*target[i] = n
	}
	return version, nil
}

func ComparePackageVersions(left, right string) int {
	a, _ := ParsePackageVersion(left)
	b, _ := ParsePackageVersion(right)
	for _, pair := range [][2]int{{a.Major, b.Major}, {a.Minor, b.Minor}, {a.Patch, b.Patch}} {
		if order := cmp.Compare(pair[0], pair[1]); order != 0 {
			return order
		}
	}
	if a.Prerelease == b.Prerelease {
		return 0
	}
	if a.Prerelease == "" {
		return 1
	}
	if b.Prerelease == "" {
		return -1
	}
	as, bs := strings.Split(a.Prerelease, "."), strings.Split(b.Prerelease, ".")
	for i := 0; i < min(len(as), len(bs)); i++ {
		an, ae := strconv.Atoi(as[i])
		bn, be := strconv.Atoi(bs[i])
		order := strings.Compare(as[i], bs[i])
		if ae == nil && be == nil {
			order = cmp.Compare(an, bn)
		} else if ae == nil {
			order = -1
		} else if be == nil {
			order = 1
		}
		if order != 0 {
			return order
		}
	}
	return cmp.Compare(len(as), len(bs))
}

func PackageDependency(requirement string) (string, string) {
	id, constraint, _ := strings.Cut(requirement, "@")
	return id, constraint
}

func PackageVersionMatches(version, constraint string) bool {
	parsedVersion, _ := ParsePackageVersion(version)
	if constraint == "" || constraint == "*" {
		return parsedVersion.Prerelease == ""
	}
	for _, alternative := range strings.Split(constraint, "||") {
		if parsedVersion.Prerelease != "" && !strings.Contains(alternative, "-") {
			continue
		}
		matched := true
		for _, term := range strings.Fields(alternative) {
			op, base := "=", term
			for _, prefix := range []string{">=", "<=", ">", "<", "^", "~", "="} {
				if strings.HasPrefix(term, prefix) {
					op, base = prefix, strings.TrimPrefix(term, prefix)
					break
				}
			}
			parsed, err := ParsePackageVersion(base)
			if err != nil {
				matched = false
				break
			}
			order := ComparePackageVersions(version, base)
			switch op {
			case "=":
				matched = matched && order == 0
			case ">":
				matched = matched && order > 0
			case ">=":
				matched = matched && order >= 0
			case "<":
				matched = matched && order < 0
			case "<=":
				matched = matched && order <= 0
			case "~", "^":
				upper := fmt.Sprintf("%d.%d.0", parsed.Major, parsed.Minor+1)
				if op == "^" {
					if parsed.Major > 0 {
						upper = fmt.Sprintf("%d.0.0", parsed.Major+1)
					} else if parsed.Minor == 0 {
						upper = fmt.Sprintf("0.0.%d", parsed.Patch+1)
					}
				}
				matched = matched && order >= 0 && ComparePackageVersions(version, upper) < 0
			}
		}
		if matched {
			return true
		}
	}
	return false
}
