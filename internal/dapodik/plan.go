package dapodik

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const NoRombelLabel = "(Tanpa Rombel)"

var romanGrades = map[string]int{
	"I": 1, "II": 2, "III": 3, "IV": 4, "V": 5, "VI": 6,
	"VII": 7, "VIII": 8, "IX": 9, "X": 10, "XI": 11, "XII": 12,
}

type Group struct {
	Label    string
	Total    int
	Empty    int
	Eligible int
	Students []Student
}

type PlanOptions struct {
	StartNIPD      *int
	SelectedGroups map[string]bool
	Level          string
}

type PlanItem struct {
	Group        string
	Nama         string
	NISN         string
	RegistrasiID string
	OldNIPD      string
	NewNIPD      string
}

type Plan struct {
	HighestNIPD   int
	StartNIPD     int
	HasNumeric    bool
	TotalStudents int
	Filled        int
	Empty         int
	NoRombel      int
	Groups        []Group
	Items         []PlanItem
	Fingerprint   string
}

const (
	NIPDOperatorGT  = ">"
	NIPDOperatorGTE = ">="
	NIPDOperatorLT  = "<"
	NIPDOperatorLTE = "<="
)

func NormalizeNIPDOperator(op string) (string, error) {
	op = strings.TrimSpace(op)
	if op == "" {
		return NIPDOperatorGT, nil
	}
	switch op {
	case NIPDOperatorGT, NIPDOperatorGTE, NIPDOperatorLT, NIPDOperatorLTE:
		return op, nil
	default:
		return "", errors.New("operator NIPD harus salah satu: >, >=, <, <=")
	}
}

func MatchesNIPDOperator(n, threshold int, op string) bool {
	normalized, err := NormalizeNIPDOperator(op)
	if err != nil {
		return false
	}
	return nipdMatchesOperator(n, threshold, normalized)
}

func nipdMatchesOperator(n, threshold int, op string) bool {
	switch op {
	case NIPDOperatorGTE:
		return n >= threshold
	case NIPDOperatorLT:
		return n < threshold
	case NIPDOperatorLTE:
		return n <= threshold
	default:
		return n > threshold
	}
}

type RenumberOptions struct {
	Threshold      int
	Operator       string
	StartNIPD      int
	SelectedGroups map[string]bool
	Level          string
}

type RenumberPlan struct {
	HighestNIPD   int
	Threshold     int
	Operator      string
	StartNIPD     int
	TotalStudents int
	Filled        int
	Empty         int
	NoRombel      int
	Eligible      int
	Kept          int
	SkippedNoReg  int
	OldMin        int
	OldMax        int
	NewMax        int
	Groups        []Group
	Items         []PlanItem
	Conflicts     []int
	Fingerprint   string
}

func studentKey(s Student) string {
	if strings.TrimSpace(s.RegistrasiID) != "" {
		return strings.TrimSpace(s.RegistrasiID)
	}
	if strings.TrimSpace(s.PesertaDidikID) != "" {
		return strings.TrimSpace(s.PesertaDidikID)
	}
	return strings.TrimSpace(s.NISN.Value) + "|" + strings.TrimSpace(s.Nama)
}

func Fingerprint(students []Student) string {
	entries := make([]string, 0, len(students))
	for _, s := range students {
		entries = append(entries, strings.Join([]string{
			studentKey(s),
			strings.TrimSpace(s.NIPD.Value),
			strings.TrimSpace(s.Rombel.Value),
		}, "\x1f"))
	}
	sort.Strings(entries)
	h := sha256.New()
	for _, e := range entries {
		_, _ = h.Write([]byte(e))
		_, _ = h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func matchesLevel(rombel, level string) bool {
	level = strings.TrimSpace(level)
	if level == "" {
		return true
	}
	r := strings.ToUpper(strings.TrimSpace(rombel))
	l := strings.ToUpper(level)
	return r == l || strings.HasPrefix(r, l+" ")
}

func splitNatural(s string) []string {
	var out []string
	var current strings.Builder
	lastDigit := false
	for i, r := range s {
		isDigit := unicode.IsDigit(r)
		if i > 0 && isDigit != lastDigit {
			out = append(out, current.String())
			current.Reset()
		}
		current.WriteRune(r)
		lastDigit = isDigit
	}
	if current.Len() > 0 {
		out = append(out, current.String())
	}
	return out
}

func naturalLess(a, b string) bool {
	as := splitNatural(strings.ToLower(a))
	bs := splitNatural(strings.ToLower(b))
	for i := 0; i < len(as) && i < len(bs); i++ {
		ai, aErr := strconv.Atoi(as[i])
		bi, bErr := strconv.Atoi(bs[i])
		if aErr == nil && bErr == nil {
			if ai != bi {
				return ai < bi
			}
			continue
		}
		if as[i] != bs[i] {
			return as[i] < bs[i]
		}
	}
	return len(as) < len(bs)
}

func rombelGradeSuffix(label string) (int, string) {
	fields := strings.Fields(strings.TrimSpace(label))
	if len(fields) == 0 {
		return 999, ""
	}
	grade, ok := romanGrades[strings.ToUpper(fields[0])]
	if !ok {
		return 999, strings.TrimSpace(label)
	}
	return grade, strings.Join(fields[1:], " ")
}

func groupLess(a, b string) bool {
	ag, as := rombelGradeSuffix(a)
	bg, bs := rombelGradeSuffix(b)
	if ag != bg {
		return ag < bg
	}
	if as != bs {
		return naturalLess(as, bs)
	}
	return strings.ToLower(a) < strings.ToLower(b)
}

func BuildPlan(students []Student, opts PlanOptions) (*Plan, error) {
	plan := &Plan{
		TotalStudents: len(students),
		Fingerprint:   Fingerprint(students),
	}

	used := make(map[int]struct{})
	numericFound := false
	for _, s := range students {
		if s.NIPD.IsEmpty() {
			continue
		}
		plan.Filled++
		if n, ok := s.NIPD.Int(); ok {
			numericFound = true
			if n > plan.HighestNIPD {
				plan.HighestNIPD = n
			}
			used[n] = struct{}{}
		}
	}
	plan.Empty = plan.TotalStudents - plan.Filled
	plan.HasNumeric = numericFound

	if opts.StartNIPD != nil {
		plan.StartNIPD = *opts.StartNIPD
	} else {
		if !numericFound {
			return nil, errors.New("tidak ada NIPD numerik; tentukan NIPD awal secara manual")
		}
		plan.StartNIPD = plan.HighestNIPD + 1
	}
	if plan.StartNIPD < 0 {
		return nil, errors.New("NIPD awal tidak boleh negatif")
	}

	groupMap := make(map[string]*Group)
	for _, s := range students {
		rombel := strings.TrimSpace(s.Rombel.Value)
		if rombel == "" {
			plan.NoRombel++
			if opts.Level != "" {
				continue
			}
			rombel = NoRombelLabel
		} else if !matchesLevel(rombel, opts.Level) {
			continue
		}
		g, ok := groupMap[rombel]
		if !ok {
			g = &Group{Label: rombel}
			groupMap[rombel] = g
		}
		g.Total++
		g.Students = append(g.Students, s)
		if s.NIPD.IsEmpty() {
			g.Empty++
		}
	}

	labels := make([]string, 0, len(groupMap))
	for label := range groupMap {
		labels = append(labels, label)
	}
	sort.Slice(labels, func(i, j int) bool { return groupLess(labels[i], labels[j]) })

	for _, label := range labels {
		g := groupMap[label]
		sort.SliceStable(g.Students, func(i, j int) bool {
			a, b := g.Students[i], g.Students[j]
			an := strings.ToLower(strings.TrimSpace(a.Nama))
			bn := strings.ToLower(strings.TrimSpace(b.Nama))
			if an != bn {
				return an < bn
			}
			return strings.TrimSpace(a.NISN.Value) < strings.TrimSpace(b.NISN.Value)
		})
		plan.Groups = append(plan.Groups, *g)
	}

	next := plan.StartNIPD
	take := func() int {
		for {
			if _, ok := used[next]; !ok {
				used[next] = struct{}{}
				n := next
				next++
				return n
			}
			next++
		}
	}

	for _, g := range plan.Groups {
		if opts.SelectedGroups != nil && !opts.SelectedGroups[g.Label] {
			continue
		}
		for _, s := range g.Students {
			if !s.NIPD.IsEmpty() {
				continue
			}
			regID := strings.TrimSpace(s.RegistrasiID)
			if regID == "" {
				continue
			}
			plan.Items = append(plan.Items, PlanItem{
				Group:        g.Label,
				Nama:         strings.TrimSpace(s.Nama),
				NISN:         strings.TrimSpace(s.NISN.Value),
				RegistrasiID: regID,
				OldNIPD:      strings.TrimSpace(s.NIPD.Value),
				NewNIPD:      strconv.Itoa(take()),
			})
		}
	}
	return plan, nil
}

// BuildRenumberPlan merencanakan penggantian NIPD lama. NIPD numerik dipilih
// berdasarkan operator terhadap Threshold. Nomor baru dialokasikan berurutan
// mulai StartNIPD, sehingga rencana harus bebas konflik sebelum apply.
func BuildRenumberPlan(students []Student, opts RenumberOptions) (*RenumberPlan, error) {
	if opts.Threshold < 0 {
		return nil, errors.New("batas NIPD tidak boleh negatif")
	}
	if opts.StartNIPD < 0 {
		return nil, errors.New("NIPD awal tidak boleh negatif")
	}
	operator, err := NormalizeNIPDOperator(opts.Operator)
	if err != nil {
		return nil, err
	}

	start := opts.StartNIPD
	base, err := BuildPlan(students, PlanOptions{StartNIPD: &start, Level: opts.Level})
	if err != nil {
		return nil, err
	}
	plan := &RenumberPlan{
		HighestNIPD:   base.HighestNIPD,
		Threshold:     opts.Threshold,
		Operator:      operator,
		StartNIPD:     opts.StartNIPD,
		TotalStudents: base.TotalStudents,
		Filled:        base.Filled,
		Empty:         base.Empty,
		NoRombel:      base.NoRombel,
		Fingerprint:   base.Fingerprint,
	}

	selected := func(label string) bool {
		return opts.SelectedGroups == nil || opts.SelectedGroups[label]
	}
	for _, group := range base.Groups {
		g := group
		g.Eligible = 0
		for _, s := range g.Students {
			n, ok := s.NIPD.Int()
			if !ok || !nipdMatchesOperator(n, opts.Threshold, operator) {
				continue
			}
			regID := strings.TrimSpace(s.RegistrasiID)
			if regID != "" {
				g.Eligible++
			}
			if !selected(g.Label) {
				continue
			}
			if regID == "" {
				plan.SkippedNoReg++
				continue
			}
			newNIPD := opts.StartNIPD + len(plan.Items)
			plan.Items = append(plan.Items, PlanItem{
				Group:        g.Label,
				Nama:         strings.TrimSpace(s.Nama),
				NISN:         strings.TrimSpace(s.NISN.Value),
				RegistrasiID: regID,
				OldNIPD:      strings.TrimSpace(s.NIPD.Value),
				NewNIPD:      strconv.Itoa(newNIPD),
			})
			if plan.OldMin == 0 || n < plan.OldMin {
				plan.OldMin = n
			}
			if n > plan.OldMax {
				plan.OldMax = n
			}
		}
		plan.Groups = append(plan.Groups, g)
	}
	plan.Eligible = len(plan.Items)
	plan.Kept = plan.TotalStudents - plan.Eligible - plan.SkippedNoReg
	if plan.Eligible > 0 {
		plan.NewMax = opts.StartNIPD + plan.Eligible - 1
	}

	changed := make(map[string]bool, len(plan.Items))
	for _, it := range plan.Items {
		changed[it.RegistrasiID] = true
	}
	conflictSet := make(map[int]struct{})
	for _, s := range students {
		n, ok := s.NIPD.Int()
		if !ok || n < opts.StartNIPD || n > plan.NewMax {
			continue
		}
		if !changed[strings.TrimSpace(s.RegistrasiID)] {
			conflictSet[n] = struct{}{}
		}
	}
	for n := range conflictSet {
		plan.Conflicts = append(plan.Conflicts, n)
	}
	sort.Ints(plan.Conflicts)
	return plan, nil
}
