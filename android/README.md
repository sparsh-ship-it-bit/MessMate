# MessMate Android app

This Android WebView wrapper loads the existing mobile scanner at:

`https://messmate-zhms.onrender.com/mobile-scan.html`

The backend and database remain hosted on Render. Camera permission is granted only to the trusted HTTPS MessMate origin.

## Build locally

Install JDK 17, Android SDK (API 35), and Gradle 8.9, then run:

```sh
cd android
gradle :app:assembleDebug
```

The APK is written to `app/build/outputs/apk/debug/app-debug.apk`.

## Build from GitHub

Open **Actions → Build MessMate Android APK → Run workflow**. Download the `MessMate-Android-APK` artifact from the completed run.

To publish an APK under **Releases**, create and push a version tag such as `v1.0.0`. The workflow attaches `MessMate-Android.apk` to that GitHub Release.

The current workflow uses Android's debug signing key so the APK is installable for direct testing. For Play Store distribution or a stable public signing identity, configure a private release keystore in GitHub Actions secrets before switching to a release-signed build.
