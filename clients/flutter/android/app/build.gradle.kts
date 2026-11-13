import java.util.Base64

plugins {
    id("com.android.application")
    id("kotlin-android")
    // The Flutter Gradle Plugin must be applied after the Android and Kotlin Gradle plugins.
    id("dev.flutter.flutter-gradle-plugin")
}

val agenthubDefines: Map<String, String> = (project.findProperty("dart-defines") as? String)
    ?.split(",")?.filter { it.isNotEmpty() }?.associate {
        val entry = String(Base64.getDecoder().decode(it), Charsets.UTF_8).split("=", limit = 2)
        entry[0] to entry.getOrElse(1) { "" }
    } ?: emptyMap()

android {
    namespace = "dev.agenthub.agenthub_client"
    compileSdk = flutter.compileSdkVersion
    ndkVersion = flutter.ndkVersion

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = JavaVersion.VERSION_17.toString()
    }

    defaultConfig {
        manifestPlaceholders["agenthubCleartextTraffic"] =
            (agenthubDefines["AGENTHUB_ALLOW_LAN_HTTP"] == "true").toString()
        // TODO: Specify your own unique Application ID (https://developer.android.com/studio/build/application-id.html).
        applicationId = "dev.agenthub.agenthub_client"
        // You can update the following values to match your application needs.
        // For more information, see: https://flutter.dev/to/review-gradle-config.
        minSdk = 24
        targetSdk = flutter.targetSdkVersion
        ndk { abiFilters += listOf("arm64-v8a", "x86_64") }
        versionCode = flutter.versionCode
        versionName = flutter.versionName
    }

    buildTypes {
        release {
            // TODO: Add your own signing config for the release build.
            // Signing with the debug keys for now, so `flutter run --release` works.
            signingConfig = signingConfigs.getByName("debug")
        }
    }
}

flutter {
    source = "../.."
}
