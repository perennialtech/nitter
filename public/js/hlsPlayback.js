// @license http://www.gnu.org/licenses/agpl-3.0.html AGPL-3.0
// SPDX-License-Identifier: AGPL-3.0-only
let hlsLibrary;

function loadHls() {
    if (!hlsLibrary) {
        hlsLibrary = new Promise((resolve, reject) => {
            const script = document.createElement("script");
            script.src = "/js/hls.min.js";
            script.onload = () => resolve(window.Hls);
            script.onerror = () => {
                script.remove();
                hlsLibrary = undefined;
                reject(new Error("Could not load HLS playback"));
            };
            document.head.appendChild(script);
        });
    }
    return hlsLibrary;
}

async function playMedia(overlay, tagName) {
    const media = overlay.parentElement.querySelector(tagName);
    const Hls = await loadHls().catch(error => console.warn(error));
    const useHls = Hls?.isSupported();
    if (media.controls || (!useHls && !media.canPlayType("application/vnd.apple.mpegurl"))) {
        return;
    }
    const url = media.getAttribute("data-url");
    const startTime = parseFloat(media.getAttribute("data-start") || "0");
    media.setAttribute("controls", "");
    overlay.style.display = "none";

    if (useHls) {
        const hls = new Hls({autoStartLoad: false});
        hls.loadSource(url);
        hls.attachMedia(media);
        hls.on(Hls.Events.MANIFEST_PARSED, function () {
            hls.loadLevel = hls.levels.length - 1;
            hls.startLoad(startTime);
            media.play();
        });
    } else {
        media.src = url;
        // canplay fires again after every seek; only the first one starts playback
        media.addEventListener('canplay', function() {
            if (startTime > 0) media.currentTime = startTime;
            media.play();
        }, {once: true});
    }
}

function playVideo(overlay) { playMedia(overlay, 'video'); }
function playAudio(overlay) { playMedia(overlay, 'audio'); }
// @license-end
