// Example only (TeamCity Kotlin DSL). Command-line build step; a non-zero exit code fails the build.
import jetbrains.buildServer.configs.kotlin.*
import jetbrains.buildServer.configs.kotlin.buildSteps.script

version = "2024.12"
project {
  buildType(BuildType {
    id("UpgradeReadiness")
    name = "Kubernetes upgrade readiness (cgroupguard)"
    params {
      param("env.CGROUPGUARD_VERSION", "0.1.1")
      param("env.BASE_URL", "https://github.com/amitpatole/cgroupguard/releases/download")
    }
    steps {
      script {
        name = "Install verified cgroupguard and check"
        scriptContent = """
          set -euo pipefail
          VERSION="${'$'}CGROUPGUARD_VERSION" BASE_URL="${'$'}BASE_URL" PUBKEY=ci/cgroupguard-cosign.pub DEST="${'$'}PWD/bin" ci/install-cgroupguard.sh
          PATH="${'$'}PWD/bin:${'$'}PATH" cgroupguard check --target-version 1.35 --exit-error --fail-on warning --format sarif > cgroupguard.sarif
        """.trimIndent()
      }
    }
    artifactRules = "cgroupguard.sarif"
  })
}
