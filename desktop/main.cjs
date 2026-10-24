'use strict';

const { app, BrowserWindow, dialog, ipcMain, shell, session } = require('electron');
const { autoUpdater } = require('electron-updater');
const fs = require('node:fs');
const path = require('node:path');

const configPath = path.join(__dirname, 'app-config.json');
const config = JSON.parse(fs.readFileSync(configPath, 'utf8'));
const APP_URL = process.env.MESSMATE_APP_URL || config.appUrl;
const APP_ORIGIN = new URL(APP_URL).origin;

if (new URL(APP_URL).protocol !== 'https:') {
  throw new Error('MessMate desktop must load the production app over HTTPS.');
}

let mainWindow;
let splashWindow;
let splashTimer;
let startupReady = false;
let updateCheckInProgress = false;

function sendUpdateStatus(status) {
  if (mainWindow && !mainWindow.isDestroyed()) {
    mainWindow.webContents.send('messmate:update-status', status);
  }
}

function isTrustedUrl(rawUrl) {
  try {
    return new URL(rawUrl).origin === APP_ORIGIN;
  } catch {
    return false;
  }
}

function closeSplashAndShowApp() {
  if (splashTimer) {
    clearTimeout(splashTimer);
    splashTimer = null;
  }
  if (splashWindow && !splashWindow.isDestroyed()) {
    splashWindow.close();
  }
  splashWindow = null;
  if (mainWindow && !mainWindow.isDestroyed()) {
    mainWindow.show();
  }
}

function createSplashWindow() {
  splashWindow = new BrowserWindow({
    width: 420,
    height: 300,
    frame: false,
    resizable: false,
    movable: false,
    show: false,
    center: true,
    backgroundColor: '#050505',
    alwaysOnTop: true,
    autoHideMenuBar: true,
    webPreferences: {
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true
    }
  });
  splashWindow.once('ready-to-show', () => {
    if (splashWindow && !splashWindow.isDestroyed()) splashWindow.show();
  });
  splashWindow.loadFile(path.join(__dirname, 'splash.html'));
}

function createWindow() {
  createSplashWindow();
  // Keep the branded intro brief even if the hosted app takes longer to load.
  splashTimer = setTimeout(closeSplashAndShowApp, 30000);

  mainWindow = new BrowserWindow({
    width: 1360,
    height: 900,
    minWidth: 980,
    minHeight: 650,
    show: false,
    backgroundColor: '#0f172a',
    autoHideMenuBar: true,
    webPreferences: {
      preload: path.join(__dirname, 'preload.cjs'),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
      webSecurity: true
    }
  });

  mainWindow.webContents.setWindowOpenHandler(({ url }) => {
    if (!isTrustedUrl(url) && url.startsWith('https://')) {
      shell.openExternal(url);
    }
    return { action: 'deny' };
  });

  mainWindow.webContents.on('will-navigate', (event, url) => {
    if (!isTrustedUrl(url)) {
      event.preventDefault();
      if (url.startsWith('https://')) shell.openExternal(url);
    }
  });

  mainWindow.once('ready-to-show', () => {
    // The renderer signals readiness after its initial dashboard data requests settle.
    // Do not reveal a half-loaded dashboard just because the HTML has painted.
    if (startupReady) closeSplashAndShowApp();
  });
  mainWindow.on('closed', () => { mainWindow = null; });
  mainWindow.loadURL(APP_URL);
}

app.whenReady().then(() => {
  // Allow camera access for the trusted HTTPS app so QR attendance scanning can work.
  session.defaultSession.setPermissionRequestHandler((webContents, permission, callback) => {
    const trustedPage = isTrustedUrl(webContents.getURL());
    callback(trustedPage && ['media', 'notifications'].includes(permission));
  });

  ipcMain.handle('messmate:get-version', () => app.getVersion());
  ipcMain.on('messmate:app-ready', (event) => {
    if (!mainWindow || event.sender !== mainWindow.webContents) return;
    startupReady = true;
    if (mainWindow.webContents.isLoading()) {
      mainWindow.webContents.once('did-finish-load', closeSplashAndShowApp);
    } else {
      closeSplashAndShowApp();
    }
  });
  ipcMain.handle('messmate:check-updates', async () => {
    if (!app.isPackaged) return { status: 'development', message: 'Updates are available in the installed desktop build.' };
    if (updateCheckInProgress) return { status: 'checking', message: 'Already checking for updates.' };
    updateCheckInProgress = true;
    try {
      await autoUpdater.checkForUpdates();
      return { status: 'checking', message: 'Checking for updates.' };
    } catch (error) {
      sendUpdateStatus({ status: 'error', message: 'Could not check for updates. Please try again later.' });
      return { status: 'error' };
    } finally {
      updateCheckInProgress = false;
    }
  });

  autoUpdater.autoDownload = true;
  autoUpdater.autoInstallOnAppQuit = true;
  autoUpdater.on('checking-for-update', () => sendUpdateStatus({ status: 'checking', message: 'Checking for updates…' }));
  autoUpdater.on('update-available', (info) => sendUpdateStatus({ status: 'available', version: info.version, message: 'An update is available and is downloading.' }));
  autoUpdater.on('download-progress', (progress) => sendUpdateStatus({ status: 'downloading', percent: Math.round(progress.percent), message: 'Downloading update…' }));
  autoUpdater.on('update-not-available', () => sendUpdateStatus({ status: 'current', message: 'MessMate is up to date.' }));
  autoUpdater.on('error', () => sendUpdateStatus({ status: 'error', message: 'Update check failed. You can continue using MessMate and try again later.' }));
  autoUpdater.on('update-downloaded', async (info) => {
    sendUpdateStatus({ status: 'downloaded', version: info.version, message: 'Update ready to install.' });
    const choice = await dialog.showMessageBox(mainWindow, {
      type: 'info',
      title: 'MessMate update ready',
      message: `MessMate ${info.version} has been downloaded.`,
      detail: 'Restart the app to finish installing the update.',
      buttons: ['Restart now', 'Later'],
      defaultId: 0,
      cancelId: 1
    });
    if (choice.response === 0) autoUpdater.quitAndInstall();
  });

  createWindow();

  if (app.isPackaged) {
    setTimeout(() => autoUpdater.checkForUpdates().catch(() => {}), 5000);
  }

  app.on('activate', () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow();
  });
});

app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') app.quit();
});
