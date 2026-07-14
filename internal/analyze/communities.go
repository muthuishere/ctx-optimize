package analyze

// Community detection: cluster the graph into architecture neighborhoods
// ("this repo is N subsystems"). Deterministic greedy modularity (Louvain-
// style local moving + coarsening): the same sorted-order label loop as
// plain label propagation, but each vote is penalized by community degree —
// measured on our own store, plain LPA collapses all code into one
// 568-node mega-community because hub files (app.go) spread one label
// everywhere; the modularity penalty is what stops that epidemic.
// Deterministic: nodes visited in sorted-id order, ties broken by smallest
// community id, no randomness. Near-linear: O(rounds × E) per level and
// levels shrink geometrically. Decision record:
// openspec/changes/2026-07-14-community-detection/.

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

const (
	moveMaxRounds = 20 // per-level round cap → guaranteed termination, still deterministic
	maxLevels     = 10 // coarsening levels (converges far earlier in practice)
	minCommunity  = 5  // smaller communities are dust: absorbed into their strongest neighbor, or dropped when disconnected
	communityHubs = 5  // top members by degree kept per community
	communityDirs = 3  // dominant source directories kept per community
)

// Community is one detected subsystem. Label is derived, never invented: the
// highest-degree member's label plus the dominant source directory.
type Community struct {
	Label   string   `json:"label"`
	Members []string `json:"members"`        // node ids, sorted
	Hubs    []string `json:"hubs"`           // top member ids by degree (tie by id)
	Dirs    []string `json:"dirs,omitempty"` // dominant source dirs (count desc, tie by name)
}

type nb struct {
	to int
	w  float64
}

// wgraph is one coarsening level: undirected adjacency (both directions
// stored, parallel edges merged, neighbor lists sorted) + self-loop weight.
type wgraph struct {
	adj  [][]nb
	self []float64
}

// Communities clusters nodes into subsystems. Deterministic across runs and
// across input order: identical graphs always yield identical output.
// Degree-0 nodes are excluded — an isolated node is noise, not a subsystem —
// and disconnected dust (< minCommunity members, no external edges) is
// dropped rather than reported as fake subsystems.
func Communities(nodes []schema.Node, edges []schema.Edge) []Community {
	// Stable universe: ids sorted ascending, so every "smallest" tie-break
	// below is exactly "smallest node id".
	sorted := make([]schema.Node, len(nodes))
	copy(sorted, nodes)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	idx := make(map[string]int, len(sorted))
	for i, n := range sorted {
		idx[n.ID] = i
	}

	g := wgraph{adj: make([][]nb, len(sorted)), self: make([]float64, len(sorted))}
	for _, e := range edges {
		a, okA := idx[e.Source]
		b, okB := idx[e.Target]
		if !okA || !okB || a == b {
			continue // dangling reference or self-loop: no clustering signal
		}
		w := e.Weight
		if w <= 0 {
			w = 1
		}
		g.adj[a] = append(g.adj[a], nb{b, w})
		g.adj[b] = append(g.adj[b], nb{a, w})
	}
	normalize(&g)

	// Degree per node (total incident weight) for hub ranking.
	deg := make([]float64, len(sorted))
	for i, ns := range g.adj {
		for _, n := range ns {
			deg[i] += n.w
		}
	}

	// Louvain levels: local moving, then coarsen communities into
	// supernodes and repeat until nothing moves.
	assign := make([]int, len(sorted)) // original node → current community
	for i := range assign {
		assign[i] = i
	}
	cur := g
	for level := 0; level < maxLevels; level++ {
		comm, moved := localMove(cur)
		if !moved {
			break
		}
		next, renum := coarsen(cur, comm)
		for v := range assign {
			assign[v] = renum[comm[assign[v]]]
		}
		if len(next.adj) == len(cur.adj) {
			break
		}
		cur = next
	}

	// Group connected nodes by final community.
	members := map[int][]int{}
	for i := range sorted {
		if len(g.adj[i]) == 0 {
			continue
		}
		members[assign[i]] = append(members[assign[i]], i)
	}

	// Dust handling: merge communities below minCommunity into the neighbor
	// they share the most edge weight with (smallest first, ties by smallest
	// community id). Disconnected dust has no neighbor to join — drop it.
	label := make([]int, len(sorted))
	copy(label, assign)
	isolated := map[int]bool{}
	for len(members) > 1 {
		type cs struct{ id, size int }
		list := make([]cs, 0, len(members))
		for id, ms := range members {
			list = append(list, cs{id, len(ms)})
		}
		sort.Slice(list, func(a, b int) bool {
			if list[a].size != list[b].size {
				return list[a].size < list[b].size
			}
			return list[a].id < list[b].id
		})
		src := -1
		for _, c := range list {
			if isolated[c.id] {
				continue
			}
			if c.size < minCommunity {
				src = c.id
			}
			break // only the smallest non-isolated community is a candidate
		}
		if src < 0 {
			break
		}
		cross := map[int]float64{}
		for _, m := range members[src] {
			for _, n := range g.adj[m] {
				if l := label[n.to]; l != src {
					cross[l] += n.w
				}
			}
		}
		if len(cross) == 0 {
			isolated[src] = true
			continue
		}
		dst, dstW := -1, 0.0
		for l, w := range cross {
			if dst < 0 || w > dstW || (w == dstW && l < dst) {
				dst, dstW = l, w
			}
		}
		for _, m := range members[src] {
			label[m] = dst
		}
		members[dst] = append(members[dst], members[src]...)
		delete(members, src)
	}
	for id := range members {
		if isolated[id] && len(members[id]) < minCommunity {
			delete(members, id) // disconnected dust: not a subsystem
		}
	}

	out := make([]Community, 0, len(members))
	for _, ms := range members {
		sort.Ints(ms)
		byDeg := append([]int(nil), ms...)
		sort.Slice(byDeg, func(a, b int) bool {
			if deg[byDeg[a]] != deg[byDeg[b]] {
				return deg[byDeg[a]] > deg[byDeg[b]]
			}
			return byDeg[a] < byDeg[b]
		})
		hubs := byDeg
		if len(hubs) > communityHubs {
			hubs = hubs[:communityHubs]
		}
		c := Community{
			Members: make([]string, len(ms)),
			Hubs:    make([]string, len(hubs)),
			Dirs:    dominantDirs(sorted, ms),
		}
		for i, m := range ms {
			c.Members[i] = sorted[m].ID
		}
		for i, h := range hubs {
			c.Hubs[i] = sorted[h].ID
		}
		// Name the subsystem by its own code: the highest-degree member that
		// is not an imported-module node (stdlib imports like "os" out-degree
		// everything but describe nothing). Fall back to the raw top member.
		name := byDeg[0]
		for _, cand := range byDeg {
			if sorted[cand].Kind != "module" {
				name = cand
				break
			}
		}
		c.Label = sorted[name].Label
		if len(c.Dirs) > 0 {
			c.Label = fmt.Sprintf("%s (%s)", c.Label, c.Dirs[0])
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Members) != len(out[j].Members) {
			return len(out[i].Members) > len(out[j].Members)
		}
		if out[i].Label != out[j].Label {
			return out[i].Label < out[j].Label
		}
		return out[i].Members[0] < out[j].Members[0]
	})
	return out
}

// normalize sorts neighbor lists and merges parallel edges — a fixed
// iteration order is what keeps every pass below deterministic.
func normalize(g *wgraph) {
	for i := range g.adj {
		ns := g.adj[i]
		sort.Slice(ns, func(a, b int) bool { return ns[a].to < ns[b].to })
		out := ns[:0]
		for _, n := range ns {
			if len(out) > 0 && out[len(out)-1].to == n.to {
				out[len(out)-1].w += n.w
			} else {
				out = append(out, n)
			}
		}
		g.adj[i] = out
	}
}

// localMove is one Louvain level: every node starts in its own community;
// nodes are visited in index order (in-place updates) and move to the
// neighbor community with the best modularity gain
// (w_i→C − Σtot(C)·k_i / 2m), ties by smallest community id. The reduce is
// order-independent, so map iteration cannot leak in; the round cap
// guarantees termination.
func localMove(g wgraph) ([]int, bool) {
	n := len(g.adj)
	k := make([]float64, n) // weighted degree, self-loops counted twice
	var m2 float64
	for i := range g.adj {
		for _, nbr := range g.adj[i] {
			k[i] += nbr.w
		}
		k[i] += 2 * g.self[i]
		m2 += k[i]
	}
	comm := make([]int, n)
	tot := make([]float64, n) // Σ k over each community's members
	for i := range comm {
		comm[i] = i
		tot[i] = k[i]
	}
	if m2 == 0 {
		return comm, false
	}
	w2c := map[int]float64{}
	anyMoved := false
	for round := 0; round < moveMaxRounds; round++ {
		changed := false
		for i := 0; i < n; i++ {
			if len(g.adj[i]) == 0 {
				continue
			}
			for c := range w2c {
				delete(w2c, c)
			}
			for _, nbr := range g.adj[i] {
				w2c[comm[nbr.to]] += nbr.w
			}
			cur := comm[i]
			tot[cur] -= k[i]
			best, bestGain := cur, w2c[cur]-tot[cur]*k[i]/m2
			for c, w := range w2c {
				gain := w - tot[c]*k[i]/m2
				if gain > bestGain || (gain == bestGain && c < best) {
					best, bestGain = c, gain
				}
			}
			tot[best] += k[i]
			if best != cur {
				comm[i] = best
				changed, anyMoved = true, true
			}
		}
		if !changed {
			break
		}
	}
	return comm, anyMoved
}

// coarsen collapses each community into a supernode. Communities are
// renumbered by smallest member index so supernode ids — and every later
// tie-break — stay deterministic.
func coarsen(g wgraph, comm []int) (wgraph, map[int]int) {
	renum := map[int]int{}
	for i := range g.adj {
		if _, ok := renum[comm[i]]; !ok {
			renum[comm[i]] = len(renum)
		}
	}
	next := wgraph{adj: make([][]nb, len(renum)), self: make([]float64, len(renum))}
	for i := range g.adj {
		ci := renum[comm[i]]
		next.self[ci] += g.self[i]
		for _, nbr := range g.adj[i] {
			cj := renum[comm[nbr.to]]
			if ci == cj {
				if i < nbr.to { // both directions stored; count internal edges once
					next.self[ci] += nbr.w
				}
			} else {
				next.adj[ci] = append(next.adj[ci], nb{cj, nbr.w})
			}
		}
	}
	normalize(&next)
	return next, renum
}

// dominantDirs returns the community's most common source directories
// (count desc, tie by name), capped at communityDirs. Adapter URIs
// (pg://db/table) and root-level sources carry no directory signal.
func dominantDirs(sorted []schema.Node, ms []int) []string {
	count := map[string]int{}
	for _, m := range ms {
		src := sorted[m].Source
		if src == "" || strings.Contains(src, "://") {
			continue
		}
		d := path.Dir(strings.ReplaceAll(src, "\\", "/"))
		if d == "." || d == "/" {
			continue
		}
		count[d]++
	}
	dirs := make([]string, 0, len(count))
	for d := range count {
		dirs = append(dirs, d)
	}
	sort.Slice(dirs, func(a, b int) bool {
		if count[dirs[a]] != count[dirs[b]] {
			return count[dirs[a]] > count[dirs[b]]
		}
		return dirs[a] < dirs[b]
	})
	if len(dirs) > communityDirs {
		dirs = dirs[:communityDirs]
	}
	return dirs
}
