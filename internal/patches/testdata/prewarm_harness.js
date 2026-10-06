// Exercises the connection-prewarm head script against a stand-in browser.
// The script path is the first argument. Any failed expectation exits non-zero.
const vm = require("vm");
const fs = require("fs");

const code = fs.readFileSync(process.argv[2], "utf8");
const failures = [];
function expect(name, ok) {
  if (!ok) failures.push(name);
}

function run(search) {
  const log = [];
  const socks = [];
  const timers = [];
  class FakeWS {
    constructor(url, protocols) {
      this.url = url;
      this.protocols = protocols;
      this.readyState = 0;
      this.listeners = {};
      socks.push(this);
      log.push("native new " + url);
    }
    addEventListener(type, fn) {
      (this.listeners[type] = this.listeners[type] || []).push(fn);
    }
    removeEventListener(type, fn) {
      this.listeners[type] = (this.listeners[type] || []).filter((f) => f !== fn);
    }
    close() {
      this.readyState = 3;
      this.emit("close", {});
    }
    emit(type, ev) {
      (this.listeners[type] || []).forEach((fn) => fn.call(this, ev || {}));
    }
  }
  const win = {
    WebSocket: FakeWS,
    fetch: (url) => {
      log.push("native fetch " + url);
      return Promise.resolve({ ok: true, fake: 1 });
    },
  };
  const ctx = {
    window: win,
    location: {
      protocol: "https:",
      host: "agy.example.com",
      origin: "https://agy.example.com",
      href: "https://agy.example.com/",
      search: search || "",
    },
    URL,
    URLSearchParams,
    Event: class {
      constructor(type) {
        this.type = type;
      }
    },
    setTimeout: (fn, ms) => {
      timers.push([fn, ms]);
      return timers.length;
    },
    clearTimeout: () => {},
  };
  vm.createContext(ctx);
  vm.runInContext(code, ctx);
  return { win, log, socks, timers };
}

const WS_URL = "wss://agy.example.com/connect-websocket";
const nativeNews = (r) => r.log.filter((x) => x.startsWith("native new")).length;
const nativeFetches = (r) => r.log.filter((x) => x.startsWith("native fetch")).length;
const flush = (r) => r.timers.filter((t) => t[1] === 0).forEach((t) => t[0]());

(async () => {
  // The warm-up request is shared and the socket is reused while connecting.
  let r = run();
  await r.win.fetch("https://agy.example.com/connect-websocket", { credentials: "include", mode: "no-cors" });
  expect("shared warm-up fetch makes one native request", nativeFetches(r) === 1);
  let opened = 0;
  let ws = new r.win.WebSocket(WS_URL);
  ws.binaryType = "arraybuffer";
  ws.onopen = () => opened++;
  expect("socket opened early is handed to the bundle", ws === r.socks[0] && nativeNews(r) === 1);
  r.socks[0].readyState = 1;
  r.socks[0].emit("open", {});
  expect("onopen fires once when the socket opens afterwards", opened === 1);

  // Claimed after it is already open: the open event is replayed.
  r = run();
  r.socks[0].readyState = 1;
  r.socks[0].emit("open", {});
  opened = 0;
  ws = new r.win.WebSocket(WS_URL);
  ws.onopen = () => opened++;
  flush(r);
  expect("onopen is replayed for an already open socket", opened === 1 && ws === r.socks[0]);

  // Open listeners added with addEventListener after the socket is open also hear it.
  r = run();
  r.socks[0].readyState = 1;
  r.socks[0].emit("open", {});
  let heard = 0;
  ws = new r.win.WebSocket(WS_URL);
  ws.addEventListener("open", () => heard++);
  flush(r);
  expect("addEventListener open is replayed for an already open socket", heard === 1);

  // A listener object with handleEvent hears the replayed open as well.
  r = run();
  r.socks[0].readyState = 1;
  r.socks[0].emit("open", {});
  let handled = 0;
  ws = new r.win.WebSocket(WS_URL);
  ws.addEventListener("open", { handleEvent() { handled++; } });
  flush(r);
  expect("handleEvent listeners hear the replayed open", handled === 1);

  // A frame that arrives before the bundle claims the socket drops it.
  r = run();
  r.socks[0].readyState = 1;
  r.socks[0].emit("open", {});
  r.socks[0].emit("message", { data: "x" });
  ws = new r.win.WebSocket(WS_URL);
  expect("a socket that already received a frame is not reused", ws !== r.socks[0] && r.socks.length === 2);

  // A frame that arrives after the bundle claims the socket does not drop it.
  r = run();
  r.socks[0].readyState = 1;
  ws = new r.win.WebSocket(WS_URL);
  r.socks[0].emit("message", { data: "test" });
  expect("adopted socket survives message event", r.socks[0].readyState === 1 && ws === r.socks[0]);

  // Other URLs and protocol arguments are not touched.
  r = run();
  ws = new r.win.WebSocket("wss://other.example.com/x");
  expect("another URL gets its own socket", ws !== r.socks[0]);
  r = run();
  ws = new r.win.WebSocket(WS_URL, ["p"]);
  expect("a constructor call with protocols gets its own socket", ws !== r.socks[0] && ws.protocols[0] === "p");

  // A closed early socket is never reused.
  r = run();
  r.socks[0].close();
  ws = new r.win.WebSocket(WS_URL);
  expect("a closed early socket is not reused", ws !== r.socks[0]);

  // A closing early socket is cleaned up and replaced with a new socket.
  r = run();
  r.socks[0].readyState = 2; // CLOSING
  ws = new r.win.WebSocket(WS_URL);
  expect("closing early socket is cleaned up and replaced", ws !== r.socks[0] && r.socks[0].readyState === 3 && r.socks.length === 2);

  // Only the first construction is served from the early socket.
  r = run();
  const first = new r.win.WebSocket(WS_URL);
  const second = new r.win.WebSocket(WS_URL);
  expect("the second construction opens its own socket", first === r.socks[0] && second !== r.socks[0]);

  // Unrelated fetches pass through untouched.
  r = run();
  await r.win.fetch("/x");
  expect("an ordinary fetch is not intercepted", r.log.includes("native fetch /x"));

  // The wrapper keeps the constants and instanceof working.
  r = run();
  expect("constants survive the wrapper", r.win.WebSocket.OPEN === 1 && r.win.WebSocket.CLOSED === 3);
  expect("instances stay instanceof the native class", new r.win.WebSocket("wss://z/y") instanceof r.socks[0].constructor);

  // An unclaimed socket is closed after the expiry timer.
  r = run();
  const expiry = r.timers.find((t) => t[1] === 60000);
  expect("an expiry timer is armed for an unclaimed socket", !!expiry);
  expiry[0]();
  expect("the unclaimed socket is closed on expiry", r.socks[0].readyState === 3);

  // The page opts out of the WebSocket transport: nothing is opened early.
  r = run("?useWebSocket=false");
  expect("useWebSocket=false opens nothing early", nativeNews(r) === 0 && nativeFetches(r) === 0);
  r = run("?wsTransport=1");
  expect("wsTransport=1 opens nothing early", nativeNews(r) === 0 && nativeFetches(r) === 0);
  r = run("?useWebSocket=true");
  expect("useWebSocket=true still opens early", nativeNews(r) === 1);
  r = run("?wsTransport=2");
  expect("wsTransport=2 still opens early", nativeNews(r) === 1);

  if (failures.length) {
    console.error("FAILED:\n- " + failures.join("\n- "));
    process.exit(1);
  }
  console.log("ok");
})();
