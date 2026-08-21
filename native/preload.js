const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('pabuddy', {
  saveApiKey: (key) => ipcRenderer.invoke('save-api-key', key),
});
