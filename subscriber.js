(() => {
  // expectingMessage is set to true
  // if the user has just submitted a message
  // and so we should scroll the next message into view when received.
  let expectingMessage = false;
  const conn = new WebSocket(`ws://${location.host}/subscribe`);

  conn.addEventListener("close", (ev) => {
    appendLog(
      `WebSocket Disconnected code: ${ev.code}, reason: ${ev.reason}`,
      true
    );
    if (ev.code !== 1001) {
      appendLog("Reconnecting in 1s", true);
      setTimeout(dial, 1000);
    }
  });
  conn.addEventListener("open", (ev) => {
    console.info("websocket connected");
  });

  // This is where we handle messages received.
  conn.addEventListener("message", (ev) => {
    if (typeof ev.data !== "string") {
      console.error("unexpected message type", typeof ev.data);
      return;
    }
    const msg = JSON.parse(ev.data);
    const p = appendLog(msg.text);
    if (expectingMessage) {
      p.scrollIntoView();
      expectingMessage = false;
    }
  });

  const messageLog = document.getElementById("message-log");
  const publishForm = document.getElementById("publish-form");
  const messageInput = document.getElementById("message-input");

  // appendLog appends the passed text to messageLog.
  function appendLog(text, error) {
    const p = document.createElement("p");
    // Adding a timestamp to each message makes the log easier to read.
    p.innerText = `${new Date().toLocaleTimeString()}: ${text}`;
    if (error) {
      p.style.color = "red";
      p.style.fontStyle = "bold";
    }
    messageLog.append(p);
    return p;
  }
  appendLog("Submit a message to get started!");

  // onsubmit publishes the message from the user when the form is submitted.
  publishForm.onsubmit = async (ev) => {
    ev.preventDefault();

    const text = messageInput.value;
    if (text === "") {
      return;
    }
    messageInput.value = "";

    expectingMessage = true;
    try {
      const msg = {
        type: "chat",
        data: {
          name: "",
          text: text,
        },
      };
      conn.send(JSON.stringify(msg));
    } catch (err) {
      appendLog(`Publish failed: ${err.message}`, true);
    }
  };
})();
