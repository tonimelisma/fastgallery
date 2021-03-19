self.addEventListener('install', (event) => {
    console.log("Service worker installed")
});

self.addEventListener('activate', (event) => {
    console.log("Service worker activated")
});

self.addEventListener('fetch', (event) => {
    try {
        return fetch(event.request)
    } catch (error) {
        return new Response(
            "<h1>No network connection</h1>Retrying...<script>setTimeout(() => { window.location.reload(1); }, 5000);</script>",
            {
                headers: {
                    'Content-type': 'text/html'
                }
            })
    }
});