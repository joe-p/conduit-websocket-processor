import WebSocket from 'websocket';
// eslint-disable-next-line new-cap
const client = new WebSocket.client();

function processData(connection: WebSocket.connection, block: any) {
  console.log(block)
  connection.sendUTF(JSON.stringify(block));
}

client.on('connectFailed', (error) => {
  console.error(`Connect Error: ${error.toString()}`);
});

client.on('connect', (connection) => {
  console.log('WebSocket Client Connected');

  connection.on('error', (error) => {
    console.error(`Connection Error: ${error.toString()}`);
  });

  connection.on('close', () => {
    console.log('Connection Closed');
  });

  connection.on('message', (message) => {
    if (message.type === 'utf8') {
      processData(connection, JSON.parse(message.utf8Data));
    }
  });
});

client.connect('ws://localhost:8888/');
