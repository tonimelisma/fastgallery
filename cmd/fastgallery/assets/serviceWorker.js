self.addEventListener("install", (event) => {
    console.log("Service worker installed");
});

self.addEventListener("activate", (event) => {
    console.log("Service worker activated");
});

self.addEventListener("fetch", (event) => {
    if (event.request.mode === "navigate") {
        event.respondWith(
            (async () => {
                try {
                    return await fetch(event.request);
                } catch (error) {
                    return new Response("<h1>No network connection</h1>Retrying...<script>setTimeout(() => { window.location.reload(1); }, 5000);</script>",
                        { status: 200, headers: { 'Content-type': 'text/html' } })
                }
            })()
        );
    }
});