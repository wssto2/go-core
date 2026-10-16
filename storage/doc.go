// Package storage keeps files on named disks.
//
// A disk is private unless it says Public. Files of a private disk are opened only through the application, after the
// route's own permission check (web.Serve); files of a public disk are at a base URL, and anyone with the link can open
// one. The application passes the root and the base URL; this package reads no configuration.
//
//	app.Disks(
//	    storage.Disk("uploads").Local("storage/uploads"),                 // private: served only through the app
//	    storage.Disk("public").Local("storage/public").Public("/media"),  // anyone with the URL can open it
//	)
//
//	documents := storage.Use(app, "uploads").Folder("vehicle-documents")
//	photos    := storage.Use(app, "public").Folder("vehicles")
//
//	documents.Put(ctx, name, file, size, mime)
//	web.Serve(c, documents, name, web.Attachment(doc.Name))  // Range, Last-Modified, Content-Disposition with filename*
//	url, _ := photos.URL(ctx, "123/photos/front.jpg")        // "/media/vehicles/123/photos/front.jpg"
//
// Folder scopes a disk to a prefix: nothing outside it can be read, listed or deleted, and a key that escapes it is
// refused. An unknown disk name fails at start-up, with the other problems Run and Check report. URL on a private disk
// is an error, Delete of a missing file is not, and Put writes the whole stream (size 0 means unknown, any other size
// is the exact length) or leaves nothing behind.
//
// Driver is what a store implements; storage/local and storage/memory are the two that ship. A driver that also has
// Open (see Opener) lets web.Serve answer byte ranges and conditional requests.
package storage
