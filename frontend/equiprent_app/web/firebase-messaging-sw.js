// Firebase Cloud Messaging service worker: shows push notifications on the
// web while EQUIPRENT is in the background. Clicking one opens the app
// (the server sets webpush.fcm_options.link from APP_PUBLIC_URL).
//
// Web push needs your Firebase web config: copy firebase-config.example.js
// to firebase-config.js (git-ignored) and fill it in. Without it this worker
// fails to install, and the app keeps working with in-app notifications.
importScripts('https://www.gstatic.com/firebasejs/10.14.1/firebase-app-compat.js');
importScripts('https://www.gstatic.com/firebasejs/10.14.1/firebase-messaging-compat.js');
importScripts('firebase-config.js');

firebase.initializeApp(self.EQUIPRENT_FIREBASE_CONFIG);
firebase.messaging();
