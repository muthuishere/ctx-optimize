package manifests

import (
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

const fixtureCsproj = `<Project Sdk="Microsoft.NET.Sdk">
  <ItemGroup>
    <PackageReference Include="Newtonsoft.Json" Version="13.0.3" />
    <PackageReference Include="Serilog">
      <Version>3.1.1</Version>
    </PackageReference>
  </ItemGroup>
  <ItemGroup>
    <ProjectReference Include="..\Lib\Lib.csproj" />
  </ItemGroup>
</Project>
`

func TestCsprojPackageAndProjectReferences(t *testing.T) {
	b := extractFixture(t, map[string]string{
		"src/Api/Api.csproj": fixtureCsproj,
		"src/Lib/Lib.csproj": `<Project Sdk="Microsoft.NET.Sdk"><ItemGroup></ItemGroup></Project>`,
	})

	// The csproj itself is anchored (no other producer walks it).
	if n := nodeByID(b, "src/Api/Api.csproj"); n == nil || n.FileType != "manifest" {
		t.Fatalf("csproj file node missing or mis-typed: %+v", n)
	}
	e := mustEdge(t, b, "src/Api/Api.csproj", "dep:nuget/Newtonsoft.Json", "declares", schema.Extracted)
	if e.Metadata["version_spec"] != "13.0.3" {
		t.Fatalf("attribute version: %v", e.Metadata)
	}
	serilog := mustEdge(t, b, "src/Api/Api.csproj", "dep:nuget/Serilog", "declares", schema.Extracted)
	if serilog.Metadata["version_spec"] != "3.1.1" {
		t.Fatalf("child-element version: %v", serilog.Metadata)
	}
	// ProjectReference: backslashes normalized, repo-relative resolution —
	// the .NET module graph (spec success check).
	mustEdge(t, b, "src/Api/Api.csproj", "src/Lib/Lib.csproj", "depends_on", schema.Extracted)
}

func TestSlnProjectList(t *testing.T) {
	sln := "Microsoft Visual Studio Solution File, Format Version 12.00\r\n" +
		`Project("{FAE04EC0-301F-11D3-BF4B-00C04F79EFBC}") = "Api", "src\Api\Api.csproj", "{AAAA0000-0000-0000-0000-000000000001}"` + "\r\n" +
		"EndProject\r\n" +
		`Project("{2150E333-8FDC-42A3-9474-1A3956D46DE8}") = "SolutionItems", "SolutionItems", "{BBBB0000-0000-0000-0000-000000000002}"` + "\r\n" +
		"EndProject\r\n"
	b := extractFixture(t, map[string]string{
		"All.sln":            sln,
		"src/Api/Api.csproj": `<Project Sdk="Microsoft.NET.Sdk"></Project>`,
	})
	mustEdge(t, b, "All.sln", "src/Api/Api.csproj", "depends_on", schema.Extracted)
	// Solution folders (no project file extension) are not projects.
	if e := findEdge(b, "All.sln", "SolutionItems", "depends_on"); e != nil {
		t.Fatal("solution folder must not become a depends_on edge")
	}
}
