# MessMate for Windows

This Electron shell opens the hosted MessMate app in a dedicated Windows desktop window. The existing web app and API remain the source of truth, so customer data continues to be stored by the current backend.

## Production URL and startup screen

The desktop app loads `https://messmate-zhms.onrender.com`. A brief black startup screen animates the `MM` mark and then the full `MessMate` name. It normally yields to the app as soon as the hosted page is ready, and has a 1.5-second maximum so the intro does not linger.

## Local development

Requires Node.js 22 or newer.

```sh
cd desktop
npm install
npm start
```

To validate the main-process JavaScript:

```sh
npm run check
```

## Build a Windows installer

Build on Windows (or use the GitHub Actions workflow):

```sh
npm install
npm run dist:win
```

The NSIS installer is written to `desktop/dist/`.

## Over-the-air updates

There are two independent update paths:

1. **Website and feature updates:** deploy the existing hosted web app. The desktop shell loads that app over HTTPS, so most frontend changes appear without reinstalling the desktop program.
2. **Desktop-shell updates:** increment `version` in `package.json`, commit the change, and push a matching Git tag such as `v1.0.1`. The `Desktop Release` GitHub Actions workflow builds a Windows installer and publishes update metadata and artifacts to a GitHub Release. Installed copies check for releases automatically and can download updates in the background.

The repository's GitHub Actions token needs the workflow's `contents: write` permission. No personal access token should be committed to the repository.

## Limitations and release safety

- The first install still requires downloading the installer.
- Desktop-shell updates require a published GitHub Release and a higher semantic version.
- Windows may show an unknown-publisher warning until a code-signing certificate is configured. Do not claim the installer is code-signed until signing is configured.
- The updater checks for published releases; it does not make an unpublished draft release available to customers.
- The desktop app requires internet access because it uses the hosted web app. Offline-first support is not part of this first version.
- Test the actual production URL, login, camera/QR scanning, payment/UPI flows, and update installation before distributing the installer.
