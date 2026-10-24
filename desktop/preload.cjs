'use strict';

const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('messMateDesktop', {
  getVersion: () => ipcRenderer.invoke('messmate:get-version'),
  checkForUpdates: () => ipcRenderer.invoke('messmate:check-updates'),
  onUpdateStatus: (callback) => {
    if (typeof callback !== 'function') return () => {};
    const listener = (_event, status) => callback(status);
    ipcRenderer.on('messmate:update-status', listener);
    return () => ipcRenderer.removeListener('messmate:update-status', listener);
  }
});
