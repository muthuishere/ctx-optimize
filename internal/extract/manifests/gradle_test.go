package manifests

import (
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

const fixtureGradle = `plugins { id 'java' }

dependencies {
    implementation 'org.apache.kafka:kafka-clients:3.6.1'
    api "com.google.guava:guava:33.0.0-jre"
    testImplementation('org.junit.jupiter:junit-jupiter:5.10.1')
    runtimeOnly "org.postgresql:postgresql:42.7.1"
    compileOnly 'org.projectlombok:lombok:1.18.30'
    implementation "com.example:dynamic:${libVersion}"
    implementation 'com.example:interpolated:$ver'
    implementation project(':core')
    implementation libs.spring.boot
}
`

func TestGradleLineShapes(t *testing.T) {
	b := extractFixture(t, map[string]string{"build.gradle": fixtureGradle})

	impl := mustEdge(t, b, "build.gradle", "dep:maven/org.apache.kafka:kafka-clients", "declares", schema.Extracted)
	if impl.Metadata["version_spec"] != "3.6.1" || impl.Metadata["scope"] != "implementation" {
		t.Fatalf("implementation metadata: %v", impl.Metadata)
	}
	mustEdge(t, b, "build.gradle", "dep:maven/com.google.guava:guava", "declares", schema.Extracted)
	junit := mustEdge(t, b, "build.gradle", "dep:maven/org.junit.jupiter:junit-jupiter", "declares", schema.Extracted)
	if junit.Metadata["scope"] != "testImplementation" {
		t.Fatalf("test scope: %v", junit.Metadata)
	}
	mustEdge(t, b, "build.gradle", "dep:maven/org.postgresql:postgresql", "declares", schema.Extracted)
	mustEdge(t, b, "build.gradle", "dep:maven/org.projectlombok:lombok", "declares", schema.Extracted)

	// Dynamic / interpolated / project() / catalog forms: skipped SILENTLY.
	for _, n := range b.Nodes {
		switch n.ID {
		case "dep:maven/com.example:dynamic", "dep:maven/com.example:interpolated":
			t.Fatalf("interpolated coordinate must be skipped: %s", n.ID)
		}
	}
}

// Kotlin DSL: the same shapes with parens and double quotes.
func TestGradleKotlinDSL(t *testing.T) {
	b := extractFixture(t, map[string]string{"build.gradle.kts": `dependencies {
    implementation("io.ktor:ktor-server-core:2.3.7")
    testImplementation(kotlin("test"))
}
`})
	mustEdge(t, b, "build.gradle.kts", "dep:maven/io.ktor:ktor-server-core", "declares", schema.Extracted)
	for _, n := range b.Nodes {
		if n.ID == "dep:maven/test" || n.Label == "test" {
			t.Fatalf("kotlin(\"test\") must not parse as a coordinate: %s", n.ID)
		}
	}
}
