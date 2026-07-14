// gradle.go — build.gradle / build.gradle.kts recognizer. NOT a Gradle
// model: Groovy/Kotlin build scripts are programs, and we have no grammar
// for Groovy in the embedded set. This is deliberate LINE-SHAPE matching for
// the overwhelmingly common literal forms only:
//
//	implementation 'g:a:v'            implementation("g:a:v")
//	api "g:a:v"                       testImplementation('g:a:v')
//	runtimeOnly / compileOnly …       (kotlin-DSL: same shapes with parens)
//
// Anything dynamic — string interpolation ("$ver", "${…}"), version
// catalogs (libs.foo.bar), variables, platform()/project() wrappers — is
// skipped SILENTLY, the literal-or-silent contract shared with the route
// recognizers. Coordinates land in the maven namespace (they are maven
// coordinates), so the same lib federates across pom.xml and gradle files.
package manifests

import (
	"regexp"
	"strings"
)

// gradleDepRe: configuration name, optional '(' , a quoted 'g:a:v' literal.
var gradleDepRe = regexp.MustCompile(
	`^(implementation|api|testImplementation|runtimeOnly|compileOnly)\s*\(?\s*(['"])([^'"]+)['"]\s*\)?\s*$`)

func extractGradle(c *collector, rel, content string) {
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		m := gradleDepRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		coord := m[3]
		if strings.ContainsAny(coord, "$({") {
			continue // interpolated / dynamic — skip silently
		}
		parts := strings.Split(coord, ":")
		if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
			continue // only the full '<group>:<artifact>:<version>' shape
		}
		id := c.depNode("maven", parts[0]+":"+parts[1])
		c.declares(rel, id, parts[2], m[1])
	}
}
