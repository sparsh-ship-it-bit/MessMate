'use strict';

const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('messMateDesktop', {
  getVersion: () => ipcRenderer.invoke('messmate:get-version'),
  getNovaStatus: () => ipcRenderer.invoke('messmate:nova-status'),
  getNovaDiagnostics: () => ipcRenderer.invoke('messmate:nova-diagnostics'),
  onNovaCommand: (callback) => {
    if (typeof callback !== 'function') return () => {};
    const listener = (_event, command) => callback(command);
    ipcRenderer.on('messmate:nova-command', listener);
    return () => ipcRenderer.removeListener('messmate:nova-command', listener);
  },
  getNovaStatus: () => ipcRenderer.invoke('messmate:nova-status'),
  onNovaCommand: (callback) => {
    if (typeof callback !== 'function') return () => {};
    const listener = (_event, command) => callback(command);
    ipcRenderer.on('messmate:nova-command', listener);
    return () => ipcRenderer.removeListener('messmate:nova-command', listener);
  },
  signalAppReady: () => ipcRenderer.send('messmate:app-ready'),
  checkForUpdates: () => ipcRenderer.invoke('messmate:check-updates'),
  onUpdateStatus: (callback) => {
    if (typeof callback !== 'function') return () => {};
    const listener = (_event, status) => callback(status);
    ipcRenderer.on('messmate:update-status', listener);
    return () => ipcRenderer.removeListener('messmate:update-status', listener);
  }
});
