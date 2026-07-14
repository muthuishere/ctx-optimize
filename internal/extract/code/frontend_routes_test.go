package code

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// Frontend router recognition: Angular, React Router (JSX + data routers),
// Vue Router. Frontend routes carry the method token ROUTE; nested children
// compose paths; non-literal paths poison the object AND its children.
func TestFrontendRoutes(t *testing.T) {
	cases := []struct {
		name        string
		files       map[string]string
		wantNodes   map[string]string // route node id → label
		wantEdges   map[string]string // "route→handler" → synthesized_by
		absentNodes []string
	}{
		{
			name: "angular-forroot-identifier",
			files: map[string]string{
				"app.module.ts": `import { RouterModule } from '@angular/router';

export class HomeComponent {}
export class AdminComponent {}
export class UsersComponent {}

const routes = [
  { path: 'home', component: HomeComponent },
  { path: 'admin', component: AdminComponent, children: [
      { path: 'users', component: UsersComponent },
      { path: DYNAMIC, component: UsersComponent },
  ]},
  { path: 'lazy', loadChildren: () => import('./lazy').then(m => m.LazyModule) },
];

export class AppModule {}
RouterModule.forRoot(routes);
`,
			},
			wantNodes: map[string]string{
				"app.module.ts::route:ROUTE /home":        "ROUTE /home",
				"app.module.ts::route:ROUTE /admin":       "ROUTE /admin",
				"app.module.ts::route:ROUTE /admin/users": "ROUTE /admin/users",
				"app.module.ts::route:ROUTE /lazy":        "ROUTE /lazy", // lazy: node, no edge
			},
			wantEdges: map[string]string{
				"app.module.ts::route:ROUTE /home→app.module.ts::HomeComponent":         "angular-route",
				"app.module.ts::route:ROUTE /admin→app.module.ts::AdminComponent":       "angular-route",
				"app.module.ts::route:ROUTE /admin/users→app.module.ts::UsersComponent": "angular-route",
			},
		},
		{
			name: "angular-providerouter-inline",
			files: map[string]string{
				"app.config.ts": `export class XComp {}
export const appConfig = {
  providers: [provideRouter([{ path: 'x', component: XComp }])],
};
`,
			},
			wantNodes: map[string]string{
				"app.config.ts::route:ROUTE /x": "ROUTE /x",
			},
			wantEdges: map[string]string{
				"app.config.ts::route:ROUTE /x→app.config.ts::XComp": "angular-route",
			},
		},
		{
			name: "react-router-jsx",
			files: map[string]string{
				"app.tsx": `function Home() { return null; }
function Users() { return null; }
function Admin() { return null; }
function Solo() { return null; }

export default function App() {
  return (
    <Routes>
      <Route path="/admin" element={<Admin/>}>
        <Route path="users" element={<Users/>} />
        <Route path={dynamicPath} element={<Users/>} />
      </Route>
      <Route path="/solo" Component={Solo} />
      <Route path="/" element={<Home/>} />
    </Routes>
  );
}
`,
			},
			wantNodes: map[string]string{
				"app.tsx::route:ROUTE /admin":       "ROUTE /admin",
				"app.tsx::route:ROUTE /admin/users": "ROUTE /admin/users",
				"app.tsx::route:ROUTE /solo":        "ROUTE /solo",
				"app.tsx::route:ROUTE /":            "ROUTE /",
			},
			wantEdges: map[string]string{
				"app.tsx::route:ROUTE /admin→app.tsx::Admin":       "react-router-route",
				"app.tsx::route:ROUTE /admin/users→app.tsx::Users": "react-router-route",
				"app.tsx::route:ROUTE /solo→app.tsx::Solo":         "react-router-route",
				"app.tsx::route:ROUTE /→app.tsx::Home":             "react-router-route",
			},
		},
		{
			name: "react-router-data",
			files: map[string]string{
				"router.tsx": `function Root() { return null; }
function Child() { return null; }
const router = createBrowserRouter([
  { path: '/', Component: Root, children: [
      { path: 'child', element: <Child/> },
  ]},
]);
`,
			},
			wantNodes: map[string]string{
				"router.tsx::route:ROUTE /":      "ROUTE /",
				"router.tsx::route:ROUTE /child": "ROUTE /child",
			},
			wantEdges: map[string]string{
				"router.tsx::route:ROUTE /→router.tsx::Root":       "react-router-route",
				"router.tsx::route:ROUTE /child→router.tsx::Child": "react-router-route",
			},
		},
		{
			name: "vue-router",
			files: map[string]string{
				"router.ts": `function Home() {}
export class About {}
const routes = [
  { path: '/', component: Home },
  { path: '/about', component: About },
];
const router = createRouter({ history: createWebHistory(), routes });
const other = createRouter({ routes: [{ path: '/inline', component: About }] });
`,
			},
			wantNodes: map[string]string{
				"router.ts::route:ROUTE /":       "ROUTE /",
				"router.ts::route:ROUTE /about":  "ROUTE /about",
				"router.ts::route:ROUTE /inline": "ROUTE /inline",
			},
			wantEdges: map[string]string{
				"router.ts::route:ROUTE /→router.ts::Home":       "vue-router-route",
				"router.ts::route:ROUTE /about→router.ts::About": "vue-router-route",
				// /inline shares the About target; edge key dedupes per pair
				"router.ts::route:ROUTE /inline→router.ts::About": "vue-router-route",
			},
		},
		{
			// Near-miss guard: path:-shaped objects OUTSIDE a recognized router
			// call, non-RouterModule .forRoot, non-Route JSX tags, and a Route
			// without a handler-ish attribute must all stay silent.
			name: "frontend-false-positive-guard",
			files: map[string]string{
				"notroutes.ts": `export class Widget {}
const opts = { path: '/not-a-route', component: Widget };
somethingElse([{ path: '/x', component: Widget }]);
Config.forRoot([{ path: '/y', component: Widget }]);
`,
				"notroutes.tsx": `function Foo() { return null; }
const a = <Item path="/item" element={<Foo/>} />;
const b = <Route path="/grouping-only" />;
const c = <Route element={<Foo/>} />;
`,
			},
			wantNodes: map[string]string{},
			wantEdges: map[string]string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir()) // hermetic machine route-pack dir
			root := t.TempDir()
			for name, content := range tc.files {
				if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			batch, err := Extract(root)
			if err != nil {
				t.Fatal(err)
			}
			if err := batch.Validate(); err != nil {
				t.Fatalf("batch failed the door: %v", err)
			}
			assertRoutes(t, batch, tc.wantNodes, tc.wantEdges, tc.absentNodes)
		})
	}
}

// assertRoutes checks exact route nodes/edges — shared by the frontend and
// route-pack suites (same shape as TestFrameworkRoutes assertions).
func assertRoutes(t *testing.T, batch *schema.Batch, wantNodes, wantEdges map[string]string, absent []string) {
	t.Helper()
	nodes, edges := routeIndex(t, batch)
	for id, label := range wantNodes {
		n, ok := nodes[id]
		if !ok {
			t.Errorf("missing route node %s", id)
			continue
		}
		if n.Label != label {
			t.Errorf("%s label = %q, want %q", id, n.Label, label)
		}
		if n.Kind != "route" {
			t.Errorf("%s kind = %s, want route", id, n.Kind)
		}
		if n.Location == "" {
			t.Errorf("%s has no location", id)
		}
	}
	for _, id := range absent {
		if _, ok := nodes[id]; ok {
			t.Errorf("route node %s must not exist", id)
		}
	}
	if len(nodes) != len(wantNodes) {
		t.Errorf("route node count = %d, want %d: %v", len(nodes), len(wantNodes), keysOf(nodes))
	}
	for k, ch := range wantEdges {
		if edges[k] != ch {
			t.Errorf("edge %s synthesized_by = %q, want %q", k, edges[k], ch)
		}
	}
	if len(edges) != len(wantEdges) {
		t.Errorf("handles edge count = %d, want %d: %v", len(edges), len(wantEdges), edges)
	}
}
