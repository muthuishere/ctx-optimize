package manifests

import (
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

const fixturePom = `<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <parent>
    <groupId>org.springframework.boot</groupId>
    <artifactId>spring-boot-starter-parent</artifactId>
    <version>3.2.0</version>
  </parent>
  <groupId>com.example</groupId>
  <artifactId>root</artifactId>
  <modules>
    <module>core</module>
    <module>web</module>
  </modules>
  <dependencies>
    <dependency>
      <groupId>org.apache.kafka</groupId>
      <artifactId>kafka-clients</artifactId>
      <version>${kafka.version}</version>
    </dependency>
    <dependency>
      <groupId>junit</groupId>
      <artifactId>junit</artifactId>
      <version>4.13.2</version>
      <scope>test</scope>
    </dependency>
  </dependencies>
  <build>
    <plugins>
      <plugin>
        <groupId>org.apache.maven.plugins</groupId>
        <artifactId>maven-compiler-plugin</artifactId>
        <version>3.11.0</version>
      </plugin>
    </plugins>
  </build>
</project>
`

func TestPomDependencies(t *testing.T) {
	b := extractFixture(t, map[string]string{"pom.xml": fixturePom})

	kafka := nodeByID(b, "dep:maven/org.apache.kafka:kafka-clients")
	if kafka == nil {
		t.Fatal("missing kafka-clients dep node")
	}
	e := mustEdge(t, b, "pom.xml", "dep:maven/org.apache.kafka:kafka-clients", "declares", schema.Extracted)
	// ${property} versions stay verbatim — spec text, never resolved.
	if e.Metadata["version_spec"] != "${kafka.version}" || e.Metadata["scope"] != "compile" {
		t.Fatalf("kafka declares metadata: %v", e.Metadata)
	}
	junit := mustEdge(t, b, "pom.xml", "dep:maven/junit:junit", "declares", schema.Extracted)
	if junit.Metadata["scope"] != "test" || junit.Metadata["version_spec"] != "4.13.2" {
		t.Fatalf("junit metadata: %v", junit.Metadata)
	}
}

func TestPomModulesParentPlugins(t *testing.T) {
	b := extractFixture(t, map[string]string{"backend/pom.xml": fixturePom})

	// Aggregator modules resolve relative to the pom's own directory.
	mustEdge(t, b, "backend/pom.xml", "backend/core/pom.xml", "depends_on", schema.Extracted)
	mustEdge(t, b, "backend/pom.xml", "backend/web/pom.xml", "depends_on", schema.Extracted)

	parent := mustEdge(t, b, "backend/pom.xml", "dep:maven/org.springframework.boot:spring-boot-starter-parent", "declares", schema.Extracted)
	if parent.Metadata["scope"] != "parent" || parent.Metadata["version_spec"] != "3.2.0" {
		t.Fatalf("parent metadata: %v", parent.Metadata)
	}
	plugin := mustEdge(t, b, "backend/pom.xml", "dep:maven/org.apache.maven.plugins:maven-compiler-plugin", "declares", schema.Extracted)
	if plugin.Metadata["scope"] != "plugin" {
		t.Fatalf("plugin scope: %v", plugin.Metadata)
	}
}
