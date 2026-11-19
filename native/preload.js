const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('pabuddy', {
  saveApiKey: (key, opts) => ipcRenderer.invoke('save-api-key', key, opts || {}),
  finishSetup: (opts) => ipcRenderer.invoke('finish-setup', opts || {}),
  openExternal: (url) => ipcRenderer.invoke('open-external', url),
});
