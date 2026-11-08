//go:build linux

package main

// Tests of procMountShowsAll (contract diagram-svg-v8, R9, requirement
// failures): the Linux-only reading of /proc/self/mountinfo that decides
// whether /proc shows every process, which gates R8's zombie rule.
//
// Every expected value below is derived from v8 R9 alone:
//   - a line is "at /proc" when its MOUNTPOINT field is exactly /proc,
//     whatever its FSTYPE; malformed lines (no " - " separator, fewer than 5
//     fields before it or fewer than 3 after it) are never at /proc, nor are
//     mounts below /proc or escaped mount points such as /proc\040x;
//   - a line at /proc is a top when no OTHER line at /proc names its ID as
//     its PARENT; only tops decide, and a covered line is ignored whatever
//     its options or FSTYPE (mountinfo is in creation order, not stacking
//     order, so neither "last line wins" nor "any line decides" holds);
//   - a line hides when one of its comma-separated SUPEROPTIONS is
//     hidepid=V with V neither 0 nor off;
//   - the result is true only when there is exactly one top at /proc, its
//     FSTYPE is exactly proc and it does not hide; false otherwise (no line
//     at /proc, several tops, a non-proc top, a hiding top);
//   - subset=pid and gid=N change nothing on their own.
//
// The listings are written by hand in the kernel's mountinfo layout
// "ID PARENT MAJ:MIN ROOT MOUNTPOINT MOUNTOPTIONS [OPTIONAL...] - FSTYPE
// SOURCE SUPEROPTIONS". Every test keeps each line's ID and PARENT as
// written; ordering tests only change the order of whole lines.

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Lines reused by several cases.
const (
	pmRootLine   = `28 1 259:2 / / rw,relatime shared:1 - ext4 /dev/nvme0n1p2 rw,errors=remount-ro`
	pmSysLine    = `24 28 0:22 / /sys rw,nosuid,nodev,noexec,relatime shared:7 - sysfs sysfs rw`
	pmBinfmtLine = `47 25 0:41 / /proc/sys/fs/binfmt_misc rw,relatime shared:26 - autofs systemd-1 rw,fd=29,pgrp=1,timeout=0,minproto=5,maxproto=5,direct`
	// pmPlainLine is the single plain proc mount of a host listing (ID 25).
	pmPlainLine  = `25 28 0:23 / /proc rw,nosuid,nodev,noexec,relatime shared:12 - proc proc rw`
	pmPlainNoOpt = `530 529 0:59 / /proc rw,nosuid,nodev,noexec,relatime - proc proc rw`

	// R9 example K1: an older hidepid=invisible proc mount (ID 464) moved
	// over a newer plain proc mount (ID 491). The hiding line is the top.
	pmK1Hiding = `464 491 0:26 / /proc rw,nosuid,nodev,noexec,relatime shared:250 - proc proc rw,hidepid=invisible`
	pmK1Plain  = `491 28 0:84 / /proc rw,nosuid,nodev,noexec,relatime shared:262 - proc proc rw`
	// R9 example L1: a plain proc mount (ID 533) over a hidepid=invisible
	// proc mount (ID 522), as unshare --mount-proc leaves it. The plain line
	// is the top.
	pmL1Hiding = `522 28 0:23 / /proc rw,nosuid,nodev,noexec,relatime shared:12 - proc proc rw,hidepid=invisible`
	pmL1Plain  = `533 522 0:85 / /proc rw,nosuid,nodev,noexec,relatime - proc proc rw`
	// R9 example: a tmpfs mount (ID 700) on top of the plain proc mount 491.
	pmTmpfsOverK1Plain = `700 491 0:90 / /proc rw,relatime - tmpfs tmpfs rw,size=1024k,mode=755`

	// Lines that are never at /proc, each carrying a hiding option and each
	// naming a /proc line's ID (25 or 530) as its PARENT where it has one,
	// so that it would wrongly cover that line if it were taken for a line
	// at /proc.
	pmSubProcHide  = `541 530 0:59 /bus /proc/bus ro,nosuid,nodev,noexec,relatime - proc proc ro,hidepid=2`
	pmEscapedHide  = `612 25 0:61 / /proc\040x rw,relatime shared:40 - proc proc rw,hidepid=2`
	pmNoSeparator  = `614 25 0:63 / /proc rw,nosuid,nodev,noexec,relatime proc proc rw,hidepid=2`
	pmFewRightHide = `615 25 0:64 / /proc rw,nosuid,nodev,noexec,relatime - proc hidepid=2`
	// Four fields before the separator: malformed.
	pmFewLeftHide = `616 25 0:65 /proc - proc proc rw,hidepid=2`
)

// pmAt builds a well-formed line at /proc with the given ID, PARENT, FSTYPE
// and SUPEROPTIONS, without optional fields.
func pmAt(id, parent int, fstype, superoptions string) string {
	return pmMount(id, parent, "/proc", fstype, superoptions)
}

// pmMount builds a well-formed line with the given mount point.
func pmMount(id, parent int, mountpoint, fstype, superoptions string) string {
	return fmt.Sprintf("%d %d 0:%d / %s rw,nosuid,nodev,noexec,relatime - %s %s %s", id, parent, id, mountpoint, fstype, fstype, superoptions)
}

// pmListing joins lines into a mountinfo text with a final newline, as the
// kernel prints it.
func pmListing(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}

type pmCase struct {
	name      string
	mountinfo string
	want      bool
	// violation names the R9 rule an implementation breaks when this case
	// fails.
	violation string
}

// pmOrders adds one case per listing order of lines (each line keeps its ID
// and PARENT): name + " (order i)".
func pmOrders(name string, want bool, violation string, orders ...[]string) []pmCase {
	var cases []pmCase
	for i, lines := range orders {
		cases = append(cases, pmCase{fmt.Sprintf("%s (order %d)", name, i+1), pmListing(lines...), want, violation})
	}
	return cases
}

func pmCases() []pmCase {
	cases := []pmCase{
		// Happy path: exactly one top at /proc, FSTYPE proc, not hiding.
		{"single plain proc at /proc in a host listing", pmListing(pmRootLine, pmSysLine, pmPlainLine, pmBinfmtLine), true,
			"a single plain proc mount at /proc must show all (R9 example)"},
		{"single plain proc at /proc without optional fields", pmListing(pmRootLine, pmPlainNoOpt), true,
			"FSTYPE must be found after the separator, not at a fixed field index"},
		{"single plain proc at /proc with several optional fields", pmListing(`25 28 0:23 / /proc rw,nosuid shared:12 master:3 propagate_from:2 unbindable - proc proc rw`), true,
			"FSTYPE must be found after the separator, whatever the number of optional fields"},
		{"last line without a final newline", pmRootLine + "\n" + pmPlainLine, true,
			"the last line counts even without a newline after it"},
		{"exactly five fields before the separator", pmListing(pmRootLine, `25 28 0:23 / /proc - proc proc rw`), true,
			"only fewer than 5 fields before the separator make a line malformed"},
		{"a line naming its own ID as PARENT is still the top", pmListing(pmRootLine, `25 25 0:23 / /proc rw,nosuid - proc proc rw`), true,
			"a line is covered only by ANOTHER line at /proc naming its ID as PARENT"},

		// Values of hidepid that do not hide.
		{"hidepid=0", pmListing(pmRootLine, pmAt(25, 28, "proc", "rw,hidepid=0")), true, "hidepid=0 does not hide"},
		{"hidepid=off", pmListing(pmRootLine, pmAt(25, 28, "proc", "rw,hidepid=off")), true, "hidepid=off does not hide"},
		{"hidepid=0 top over a plain proc", pmListing(pmAt(901, 900, "proc", "rw,hidepid=0"), pmAt(900, 28, "proc", "rw")), true, "hidepid=0 does not hide"},

		// Only SUPEROPTIONS hide.
		{"hidepid in MOUNTOPTIONS only", pmListing(pmRootLine, `25 28 0:23 / /proc rw,nosuid,hidepid=2 - proc proc rw`), true,
			"only a hidepid option among the SUPEROPTIONS hides"},
		{"hidepid as SOURCE only", pmListing(pmRootLine, `25 28 0:23 / /proc rw,nosuid - proc hidepid=2 rw`), true,
			"only a hidepid option among the SUPEROPTIONS hides"},
		{"an option merely ending in hidepid=2", pmListing(pmRootLine, pmAt(25, 28, "proc", "rw,xhidepid=2")), true,
			"an option hides only when it is hidepid=V"},

		// subset=pid and gid=N do not change the result on their own.
		{"subset=pid alone", pmListing(pmRootLine, pmAt(25, 28, "proc", "rw,subset=pid")), true, "subset=pid alone does not hide"},
		{"subset=pid with hidepid=2", pmListing(pmRootLine, pmAt(25, 28, "proc", "rw,subset=pid,hidepid=2")), false, "hidepid=2 hides even with subset=pid"},
		{"subset=pid with hidepid=off", pmListing(pmRootLine, pmAt(25, 28, "proc", "rw,hidepid=off,subset=pid")), true, "hidepid=off does not hide, with or without subset=pid"},
		{"gid alone", pmListing(pmRootLine, pmAt(25, 28, "proc", "rw,gid=1000")), true, "gid=N alone does not hide"},
		{"gid before hidepid=2", pmListing(pmRootLine, pmAt(25, 28, "proc", "rw,gid=1000,hidepid=2")), false, "hidepid=2 hides even with gid=N"},
		{"hidepid=invisible before gid", pmListing(pmRootLine, pmAt(25, 28, "proc", "rw,hidepid=invisible,gid=27")), false, "hidepid=invisible hides even with gid=N"},
		{"gid with hidepid=0", pmListing(pmRootLine, pmAt(25, 28, "proc", "rw,gid=27,hidepid=0")), true, "hidepid=0 does not hide, with or without gid=N"},
		{"subset=pid on the plain top over a hiding mount", pmListing(pmL1Hiding, pmAt(534, 522, "proc", "rw,subset=pid")), true,
			"subset=pid on the top does not hide, and the covered hiding line is ignored"},

		// No line at /proc.
		{"empty input", "", false, "no line at /proc (empty input) gives false"},
		{"only newlines", "\n\n", false, "no line at /proc (blank lines) gives false"},
		{"only other mount points", pmListing(pmRootLine, pmSysLine, pmBinfmtLine), false, "no line at /proc gives false"},
		{"escaped mount point /proc\\040x", pmListing(pmRootLine, `612 28 0:61 / /proc\040x rw,relatime shared:40 - proc proc rw`), false,
			"MOUNTPOINT /proc\\040x is not /proc"},
		{"escaped mount point /proc\\011", pmListing(pmRootLine, `612 28 0:61 / /proc\011 rw,relatime - proc proc rw`), false,
			"MOUNTPOINT /proc\\011 is not /proc"},
		{"mount point /procx", pmListing(pmRootLine, `612 28 0:61 / /procx rw,relatime - proc proc rw`), false,
			"MOUNTPOINT must be exactly /proc, not a prefix match"},
		{"mount point /proc/", pmListing(pmRootLine, `612 28 0:61 / /proc/ rw,relatime - proc proc rw`), false,
			"MOUNTPOINT must be exactly /proc"},
		{"proc mounted at / only", pmListing(`612 28 0:61 / / rw,relatime - proc proc rw`), false,
			"a proc mount elsewhere than /proc is not at /proc"},
		{"only a proc sub-mount below /proc", pmListing(pmRootLine, `541 530 0:59 /bus /proc/bus ro,nosuid - proc proc ro`), false,
			"a proc mount below /proc is not at /proc"},

		// Non-proc tops.
		{"/proc carries only tmpfs", pmListing(pmRootLine, `613 28 0:62 / /proc rw,relatime shared:41 - tmpfs tmpfs rw,size=65536k,mode=755`), false,
			"a top whose FSTYPE is not proc gives false"},
		{"/proc carries only sysfs", pmListing(pmRootLine, `613 28 0:62 / /proc rw,relatime - sysfs sysfs rw`), false,
			"a top whose FSTYPE is not proc gives false"},
		{"/proc carries only procfs (not exactly proc)", pmListing(pmRootLine, `613 28 0:62 / /proc rw,relatime - procfs procfs rw`), false,
			"the top's FSTYPE must be exactly proc"},

		// Lines that are not at /proc neither count nor cover a line at
		// /proc, even when they name its ID as PARENT and carry hidepid.
		{"escaped /proc\\040x with hidepid over a plain /proc", pmListing(pmRootLine, pmEscapedHide, pmPlainLine), true,
			"a line at /proc\\040x is not at /proc: it neither hides nor covers the plain /proc line"},
		{"proc sub-mount below /proc with hidepid over a plain /proc", pmListing(pmRootLine, pmPlainNoOpt, pmSubProcHide), true,
			"a mount below /proc neither hides nor covers the line at /proc"},
		{"binfmt_misc sub-mount under a plain /proc", pmListing(pmRootLine, pmPlainLine, pmBinfmtLine), true,
			"a mount below /proc does not cover the line at /proc"},
		{"plain /proc/sys line over a hiding /proc", pmListing(pmRootLine, pmAt(25, 28, "proc", "rw,hidepid=2"), pmMount(26, 25, "/proc/sys", "proc", "rw")), false,
			"a mount below /proc does not cover a hiding top at /proc"},
		{"plain escaped line over a hiding /proc", pmListing(pmRootLine, pmAt(25, 28, "proc", "rw,hidepid=2"), `612 25 0:61 / /proc\040x rw,relatime - proc proc rw`), false,
			"a line at /proc\\040x does not cover a hiding top at /proc"},

		// Malformed lines are ignored: they neither hide nor cover.
		{"no separator, hidepid, over a plain /proc", pmListing(pmNoSeparator, pmPlainLine), true,
			"a line without the ' - ' separator is ignored"},
		{"too few fields after the separator, hidepid, over a plain /proc", pmListing(pmFewRightHide, pmPlainLine), true,
			"a line with fewer than three fields after the separator is ignored"},
		{"four fields before the separator, hidepid, over a plain /proc", pmListing(pmFewLeftHide, pmPlainLine), true,
			"a line with fewer than five fields before the separator is ignored"},
		{"only a plain /proc line without separator", pmListing(pmRootLine, `25 28 0:23 / /proc rw,nosuid,nodev,noexec,relatime proc proc rw`), false,
			"a malformed line is not at /proc"},
		{"only a /proc line with too few fields after the separator", pmListing(pmRootLine, `25 28 0:23 / /proc rw,nosuid - proc proc`), false,
			"a malformed line is not at /proc"},
		{"only a /proc line with four fields before the separator", pmListing(pmRootLine, `25 28 0:23 /proc - proc proc rw`), false,
			"a malformed line is not at /proc"},
		{"only a separator", pmListing(" - "), false, "a malformed line is not at /proc"},

		// Several tops and no top.
		{"three unrelated plain proc mounts", pmListing(pmPlainLine, pmPlainNoOpt, pmAt(800, 28, "proc", "rw")), false,
			"several tops give false, even when none hides"},
		{"two plain tops over one hiding base", pmListing(pmL1Hiding, pmL1Plain, pmAt(534, 522, "proc", "rw")), false,
			"two lines over the same base are two tops"},
		{"two stacks with plain tops", pmListing(pmK1Plain, pmAt(701, 491, "proc", "rw"), pmL1Hiding, pmL1Plain), false,
			"the tops of two separate stacks are several tops"},
		{"two lines at /proc covering each other", pmListing(pmAt(800, 801, "proc", "rw"), pmAt(801, 800, "proc", "rw")), false,
			"exactly one top is required; two lines covering each other leave none"},

		// Deeper stacks: only the top decides.
		{"hiding, hiding, plain top", pmListing(pmAt(900, 28, "proc", "rw,hidepid=2"), pmAt(901, 900, "proc", "rw,hidepid=invisible"), pmAt(902, 901, "proc", "rw")), true,
			"covered hiding lines are ignored, however many"},
		{"plain, plain, hiding top", pmListing(pmAt(900, 28, "proc", "rw"), pmAt(901, 900, "proc", "rw"), pmAt(902, 901, "proc", "rw,hidepid=2")), false,
			"a hiding top gives false"},
		{"plain, hiding, plain top listed top first", pmListing(pmAt(902, 901, "proc", "rw"), pmAt(901, 900, "proc", "rw,hidepid=1"), pmAt(900, 28, "proc", "rw")), true,
			"a hiding line in the middle of a stack is covered and ignored"},
		{"hiding, tmpfs, plain top", pmListing(pmL1Hiding, pmAt(540, 522, "tmpfs", "rw,hidepid=2"), pmAt(541, 540, "proc", "rw")), true,
			"covered lines are ignored whatever their FSTYPE or options"},
	}

	// R9 examples, each in more than one listing order.
	cases = append(cases, pmOrders("K1: hidepid=invisible moved over a plain proc mount", false,
		"the top (464, hidepid=invisible) hides; neither the last line nor the plain line decides",
		[]string{pmRootLine, pmK1Plain, pmK1Hiding},
		[]string{pmRootLine, pmK1Hiding, pmK1Plain},
		[]string{pmK1Hiding, pmSysLine, pmK1Plain, pmRootLine},
		[]string{pmK1Plain, pmK1Hiding},
	)...)
	cases = append(cases, pmOrders("L1: unshare --mount-proc plain proc over hidepid=invisible", true,
		"the top (533) is plain; the covered hiding line (522) must be ignored",
		[]string{pmRootLine, pmL1Hiding, pmL1Plain},
		[]string{pmRootLine, pmL1Plain, pmL1Hiding},
		[]string{pmL1Plain, pmSysLine, pmL1Hiding, pmRootLine},
		[]string{pmL1Hiding, pmL1Plain},
	)...)
	cases = append(cases, pmOrders("tmpfs on top of a plain proc mount", false,
		"a top whose FSTYPE is not proc gives false, though a plain proc line is under it",
		[]string{pmRootLine, pmK1Plain, pmTmpfsOverK1Plain},
		[]string{pmRootLine, pmTmpfsOverK1Plain, pmK1Plain},
	)...)
	cases = append(cases, pmOrders("sysfs on top of the L1 stack", false,
		"a non-proc top gives false",
		[]string{pmL1Hiding, pmL1Plain, pmAt(710, 533, "sysfs", "rw")},
		[]string{pmAt(710, 533, "sysfs", "rw"), pmL1Plain, pmL1Hiding},
	)...)
	cases = append(cases, pmOrders("procfs on top of a plain proc mount", false,
		"the top's FSTYPE must be exactly proc",
		[]string{pmK1Plain, pmAt(711, 491, "procfs", "rw")},
		[]string{pmAt(711, 491, "procfs", "rw"), pmK1Plain},
	)...)
	cases = append(cases, pmOrders("covered tmpfs with hidepid under a plain proc top", true,
		"a covered non-proc line is ignored, whatever its options",
		[]string{pmRootLine, pmAt(600, 28, "tmpfs", "rw,mode=755,hidepid=2"), pmAt(601, 600, "proc", "rw")},
		[]string{pmRootLine, pmAt(601, 600, "proc", "rw"), pmAt(600, 28, "tmpfs", "rw,mode=755,hidepid=2")},
	)...)
	cases = append(cases, pmOrders("two unrelated plain proc mounts", false,
		"several tops give false",
		[]string{pmPlainNoOpt, pmPlainLine},
		[]string{pmPlainLine, pmPlainNoOpt},
	)...)
	cases = append(cases, pmOrders("plain and hiding proc mounts side by side", false,
		"several tops give false",
		[]string{pmRootLine, pmPlainLine, pmAt(800, 28, "proc", "rw,hidepid=2")},
		[]string{pmRootLine, pmAt(800, 28, "proc", "rw,hidepid=2"), pmPlainLine},
	)...)
	cases = append(cases, pmOrders("K1 stack beside an unrelated plain proc mount", false,
		"several tops give false",
		[]string{pmK1Plain, pmK1Hiding, pmPlainNoOpt},
		[]string{pmPlainNoOpt, pmK1Hiding, pmK1Plain},
	)...)
	cases = append(cases, pmOrders("L1 stack beside an unrelated plain proc mount", false,
		"several tops give false, even when each is plain",
		[]string{pmL1Hiding, pmL1Plain, pmPlainNoOpt},
		[]string{pmPlainNoOpt, pmL1Plain, pmL1Hiding},
	)...)
	cases = append(cases, pmOrders("malformed /proc line over a plain top", true,
		"a malformed line does not cover the line at /proc",
		[]string{pmPlainLine, `26 25 0:24 / /proc rw,nosuid proc proc rw`},
		[]string{`26 25 0:24 / /proc rw,nosuid proc proc rw`, pmPlainLine},
	)...)

	// Every value of hidepid other than 0 and off hides on the top, and is
	// ignored when the line is covered.
	for _, value := range []string{"1", "2", "4", "noaccess", "invisible", "ptraceable"} {
		for _, superoptions := range []string{"hidepid=" + value, "rw,hidepid=" + value, "rw,hidepid=" + value + ",gid=0"} {
			cases = append(cases, pmCase{
				name:      "single line hiding with " + superoptions,
				mountinfo: pmListing(pmRootLine, pmAt(25, 28, "proc", superoptions), pmBinfmtLine),
				want:      false,
				violation: "hidepid=" + value + " on the top hides processes",
			})
		}
		cases = append(cases, pmOrders("hidepid="+value+" top over a plain proc", false,
			"hidepid="+value+" on the top hides processes",
			[]string{pmAt(900, 28, "proc", "rw"), pmAt(901, 900, "proc", "rw,hidepid="+value)},
			[]string{pmAt(901, 900, "proc", "rw,hidepid="+value), pmAt(900, 28, "proc", "rw")},
		)...)
		cases = append(cases, pmOrders("plain proc top over hidepid="+value, true,
			"a covered hidepid="+value+" line is ignored",
			[]string{pmAt(900, 28, "proc", "rw,hidepid="+value), pmAt(901, 900, "proc", "rw")},
			[]string{pmAt(901, 900, "proc", "rw"), pmAt(900, 28, "proc", "rw,hidepid="+value)},
		)...)
	}
	return cases
}

// failures (R9): procMountShowsAll applies every rule of v8 R9 to
// hand-written mountinfo listings. Each case names the rule whose violation
// fails it.
func TestProcMountShowsAll(t *testing.T) {
	for _, tc := range pmCases() {
		t.Run(tc.name, func(t *testing.T) {
			if got := procMountShowsAll(tc.mountinfo); got != tc.want {
				t.Errorf("procMountShowsAll = %v, want %v (%s)\nmountinfo:\n%s", got, tc.want, tc.violation, tc.mountinfo)
			}
		})
	}
}

// failures (R9 v8, skip helper): the zombie test's own mountinfo reading
// (pgProcMountRestriction in process_group_test.go) follows the same
// top-of-stack rule as R9 on every table case, so the zombie test neither
// skips on a /proc that R9 says shows all (such as L1) nor runs on one that
// R9 says hides (such as K1). Fails if the helper still applies "any hiding
// line" or "last line wins".
func TestZombieSkipReadingFollowsR9(t *testing.T) {
	for _, tc := range pmCases() {
		reason := pgProcMountRestriction(tc.mountinfo)
		if (reason == "") != tc.want {
			t.Errorf("%s: pgProcMountRestriction = %q, want a full /proc to be %v (%s)\nmountinfo:\n%s", tc.name, reason, tc.want, tc.violation, tc.mountinfo)
		}
	}
}

// pmPermute calls visit with every order of lines (Heap's algorithm). The
// slice passed to visit is reused.
func pmPermute(lines []string, visit func([]string)) {
	order := slices.Clone(lines)
	counters := make([]int, len(order))
	visit(order)
	for i := 0; i < len(order); {
		if counters[i] < i {
			if i%2 == 0 {
				order[0], order[i] = order[i], order[0]
			} else {
				order[counters[i]], order[i] = order[i], order[counters[i]]
			}
			visit(order)
			counters[i]++
			i = 0
		} else {
			counters[i] = 0
			i++
		}
	}
}

// failures (R9, ordering): mountinfo is in creation order, not stacking
// order, so the result depends only on which lines are present, never on
// their order. Every table case, with its lines (each keeping its ID and
// PARENT) put in every order (or, for more than 7 lines, reversed and
// rotated), gives the table's expected value. Fails when, for example, the
// first or the last /proc line decides, or when a line is taken to cover
// only the lines listed before it.
func TestProcMountShowsAllIgnoresLineOrder(t *testing.T) {
	for _, tc := range pmCases() {
		lines := strings.Split(tc.mountinfo, "\n")
		check := func(order []string) {
			t.Helper()
			mountinfo := strings.Join(order, "\n")
			if got := procMountShowsAll(mountinfo); got != tc.want {
				t.Fatalf("%s, lines reordered: procMountShowsAll = %v, want %v (%s)\nmountinfo:\n%s", tc.name, got, tc.want, tc.violation, mountinfo)
			}
		}
		if len(lines) <= 7 {
			pmPermute(lines, check)
			continue
		}
		reversed := slices.Clone(lines)
		slices.Reverse(reversed)
		for _, base := range [][]string{lines, reversed} {
			for shift := range base {
				check(append(slices.Clone(base[shift:]), base[:shift]...))
			}
		}
	}
}

// pmLabelled is a mountinfo line with what R9 says about it: whether it is
// at /proc, its ID and PARENT, whether its FSTYPE is proc and whether it
// hides processes.
type pmLabelled struct {
	line   string
	atProc bool
	id     string
	parent string
	isProc bool
	hides  bool
}

// pmExpected computes R9's result from the labels of a set of lines: true
// only when exactly one line at /proc is not named as PARENT by another line
// at /proc, and that top is a proc mount that does not hide.
func pmExpected(lines []pmLabelled) bool {
	var tops []pmLabelled
	for i, line := range lines {
		if !line.atProc {
			continue
		}
		covered := false
		for j, other := range lines {
			if j != i && other.atProc && other.parent == line.id {
				covered = true
			}
		}
		if !covered {
			tops = append(tops, line)
		}
	}
	return len(tops) == 1 && tops[0].isProc && !tops[0].hides
}

// failures (R9, stacking and ordering over many listings): for every subset
// of a pool of labelled lines (IDs and PARENTs fixed, forming stacks at
// /proc: plain 100 <- hiding 101 <- plain 102, tmpfs 103 over 100, and
// hidepid=0 104 <- ptraceable 108, plus lines that are not at /proc but name
// a /proc line as PARENT), the subset and its reverse in every rotation give
// the labels' result. Covers covered hiding lines, several tops, non-proc
// tops and ignored lines together. Fails when any line at /proc decides,
// when the last line decides, or when lines not at /proc cover.
func TestProcMountShowsAllStacksInAnyOrder(t *testing.T) {
	pool := []pmLabelled{
		{pmRootLine, false, "28", "1", false, false},
		{pmAt(100, 28, "proc", "rw"), true, "100", "28", true, false},
		{pmAt(101, 100, "proc", "rw,hidepid=invisible"), true, "101", "100", true, true},
		{pmAt(102, 101, "proc", "rw,subset=pid"), true, "102", "101", true, false},
		{pmAt(103, 100, "tmpfs", "rw,mode=755"), true, "103", "100", false, false},
		{pmAt(104, 28, "proc", "rw,hidepid=0"), true, "104", "28", true, false},
		{pmAt(108, 104, "proc", "rw,gid=27,hidepid=ptraceable"), true, "108", "104", true, true},
		{pmMount(105, 100, "/proc/sys", "proc", "ro,hidepid=2"), false, "105", "100", true, true},
		{`106 102 0:30 / /proc rw proc proc rw,hidepid=2`, false, "106", "102", true, true},
		{`107 101 0:31 / /proc\040x rw - proc proc rw`, false, "107", "101", true, false},
		{`109 100 0:32 /proc - proc proc rw`, false, "109", "100", true, false},
	}
	for mask := 0; mask < 1<<len(pool); mask++ {
		var subset []pmLabelled
		for i, item := range pool {
			if mask&(1<<i) != 0 {
				subset = append(subset, item)
			}
		}
		want := pmExpected(subset)
		reversed := slices.Clone(subset)
		slices.Reverse(reversed)
		for _, order := range [][]pmLabelled{subset, reversed} {
			for shift := 0; shift < max(len(order), 1); shift++ {
				lines := make([]string, 0, len(order))
				for i := range order {
					lines = append(lines, order[(i+shift)%len(order)].line)
				}
				mountinfo := strings.Join(lines, "\n")
				if got := procMountShowsAll(mountinfo); got != want {
					t.Fatalf("procMountShowsAll = %v, want %v by the top-of-stack rule\nmountinfo:\n%s", got, want, mountinfo)
				}
			}
		}
	}
}

// pmFreshIDs returns n distinct decimal IDs below 2^31 none of which occurs
// anywhere in text as a substring, so no line of text can name one as its ID
// or PARENT (not even with leading zeros or a sign).
func pmFreshIDs(text string, n int) []string {
	var ids []string
	for candidate := 1999990001; len(ids) < n; candidate++ {
		id := strconv.Itoa(candidate)
		if !strings.Contains(text, id) {
			ids = append(ids, id)
		}
	}
	return ids
}

// pmCanonicalID reports whether s is a plain decimal of at most 9 digits
// without leading zeros, which any reading of an ID compares the same way.
func pmCanonicalID(s string) bool {
	if s == "" || len(s) > 9 || (len(s) > 1 && s[0] == '0') {
		return false
	}
	for _, c := range []byte(s) {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// pmSameID reports whether two ID texts could be read as the same ID, as
// text or as numbers.
func pmSameID(a, b string) bool {
	if a == b {
		return true
	}
	x, errX := strconv.ParseInt(a, 10, 64)
	y, errY := strconv.ParseInt(b, 10, 64)
	return errX == nil && errY == nil && x == y
}

// pmUnambiguousAtProc returns the ID and PARENT of a line that every reading
// of R9 takes as a well-formed line at /proc: printable ASCII fields joined
// by single spaces, exactly one "-" field, at least 5 fields before it and 3
// after it, MOUNTPOINT exactly /proc, and canonical decimal ID and PARENT.
func pmUnambiguousAtProc(line string) (id, parent string, ok bool) {
	for _, c := range []byte(line) {
		if c < ' ' || c > '~' {
			return "", "", false
		}
	}
	fields := strings.Split(line, " ")
	if slices.Contains(fields, "") {
		return "", "", false
	}
	separator := slices.Index(fields, "-")
	if separator < 5 || len(fields)-separator-1 < 3 || slices.Index(fields[separator+1:], "-") >= 0 {
		return "", "", false
	}
	if fields[4] != "/proc" || !pmCanonicalID(fields[0]) || !pmCanonicalID(fields[1]) {
		return "", "", false
	}
	return fields[0], fields[1], true
}

// pmToken turns fuzzed text into one SUPEROPTIONS field: printable ASCII
// without spaces, never empty and never the separator "-".
func pmToken(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if c > ' ' && c <= '~' {
			b.WriteByte(c)
		}
	}
	if token := b.String(); token != "" && token != "-" {
		return token
	}
	return "rw"
}

// failures (R9, any input): metamorphic rules that hold for every mountinfo
// text X under the top-of-stack rule, seeded with every table case. Lines
// are joined by newlines, so adding a line before or after X leaves every
// line of X as it was, with its ID and PARENT.
//   - Order: X with its lines reversed, or rotated by one, gives the same
//     result as X.
//   - Ignored lines: X with a line that is never at /proc added (a mount
//     below /proc, an escaped mount point, a malformed line) gives the same
//     result as X.
//   - Covered line: X with a line at /proc added that an existing line L at
//     /proc of X covers (its ID is L's PARENT, its PARENT is a fresh ID) gives
//     the same result as X, whatever its FSTYPE and options (hidepid
//     included). Fails when a covered hiding line still gives false, or when
//     the last line decides.
//   - New hiding top: X with a hiding proc line at /proc added whose ID is
//     fresh gives false, whether it sits over a line of X (above the current
//     top or above a covered line) or stands alone: it is a top, so either it
//     is the only top and hides, or there are several tops. The same holds
//     for a non-proc line with a fresh ID.
//   - Several tops: when X gives true, X with an unrelated plain proc line
//     at /proc added (fresh ID and PARENT) gives false.
func FuzzProcMountShowsAll(f *testing.F) {
	for _, tc := range pmCases() {
		f.Add(tc.mountinfo, "rw,hidepid=2")
	}
	ignored := []string{pmSubProcHide, pmEscapedHide, pmNoSeparator, pmFewRightHide, pmFewLeftHide, pmBinfmtLine, pmRootLine, ""}
	f.Fuzz(func(t *testing.T, mountinfo, options string) {
		base := procMountShowsAll(mountinfo)
		superoptions := pmToken(options)
		fresh := pmFreshIDs(mountinfo, 2)
		lines := strings.Split(mountinfo, "\n")

		same := func(rule, text string) {
			t.Helper()
			if got := procMountShowsAll(text); got != base {
				t.Errorf("%s: procMountShowsAll = %v, want %v as for the listing without the change\nmountinfo:\n%s", rule, got, base, text)
			}
		}
		falseFor := func(rule, text string) {
			t.Helper()
			if procMountShowsAll(text) {
				t.Errorf("%s: procMountShowsAll = true, want false\nmountinfo:\n%s", rule, text)
			}
		}

		reversed := slices.Clone(lines)
		slices.Reverse(reversed)
		same("lines reversed", strings.Join(reversed, "\n"))
		same("lines rotated by one", strings.Join(append(slices.Clone(lines[1:]), lines[0]), "\n"))

		for i, line := range ignored {
			same(fmt.Sprintf("ignored line %d appended", i), mountinfo+"\n"+line)
			same(fmt.Sprintf("ignored line %d prepended", i), line+"\n"+mountinfo)
		}

		// A new top with a fresh ID: alone, and over up to three lines at
		// /proc of X.
		parents := []string{fresh[1]}
		var atProc [][2]string
		for _, line := range lines {
			if id, parent, ok := pmUnambiguousAtProc(line); ok && len(atProc) < 3 {
				atProc = append(atProc, [2]string{id, parent})
				parents = append(parents, id)
			}
		}
		for _, parent := range parents {
			for _, top := range []string{
				fmt.Sprintf("%s %s 0:7 / /proc rw,nosuid - proc proc rw,hidepid=2", fresh[0], parent),
				fmt.Sprintf("%s %s 0:7 / /proc rw,nosuid - proc proc %s,hidepid=invisible", fresh[0], parent, superoptions),
				fmt.Sprintf("%s %s 0:7 / /proc rw,nosuid - tmpfs tmpfs %s", fresh[0], parent, superoptions),
			} {
				falseFor("new hiding or non-proc top over "+parent+" appended", mountinfo+"\n"+top)
				falseFor("new hiding or non-proc top over "+parent+" prepended", top+"\n"+mountinfo)
			}
		}

		// A line at /proc covered by a line L of X. Skipped when another
		// line naming /proc already has L's PARENT as its ID, so the added
		// line never shares an ID with a line at /proc.
		for _, pair := range atProc {
			covering := pair[1]
			taken := false
			for _, line := range lines {
				if !strings.Contains(line, "/proc") {
					continue
				}
				for _, first := range []string{strings.SplitN(line, " ", 2)[0], pmFirstField(line)} {
					taken = taken || pmSameID(first, covering)
				}
			}
			if taken {
				continue
			}
			for _, covered := range []string{
				fmt.Sprintf("%s %s 0:8 / /proc rw,nosuid - proc proc rw,hidepid=2", covering, fresh[1]),
				fmt.Sprintf("%s %s 0:8 / /proc rw,nosuid - proc proc %s", covering, fresh[1], superoptions),
				fmt.Sprintf("%s %s 0:8 / /proc rw,nosuid - tmpfs tmpfs %s", covering, fresh[1], superoptions),
			} {
				same("covered line under "+covering+" appended", mountinfo+"\n"+covered)
				same("covered line under "+covering+" prepended", covered+"\n"+mountinfo)
			}
		}

		if base {
			unrelated := fmt.Sprintf("%s %s 0:9 / /proc rw,nosuid - proc proc rw", fresh[0], fresh[1])
			falseFor("unrelated plain proc top appended to a listing that shows all", mountinfo+"\n"+unrelated)
			falseFor("unrelated plain proc top prepended to a listing that shows all", unrelated+"\n"+mountinfo)
		}
	})
}

// pmFirstField returns the first whitespace-separated field of line, or "".
func pmFirstField(line string) string {
	if fields := strings.Fields(line); len(fields) > 0 {
		return fields[0]
	}
	return ""
}
