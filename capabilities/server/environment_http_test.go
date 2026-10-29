package platformserver

// The same lifecycle as TestApplicationLifecycleAcrossEnvironments, driven
// through the HTTP surface a browser and the host console use:
// builder routes for drafts, preview, save and activation; host-console routes
// for support sessions, promotion and migration. It is the contract the web
// release workbench and the host console are written against.
