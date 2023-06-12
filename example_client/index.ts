// eslint-disable-next-line import/no-extraneous-dependencies
import WebSocket from 'websocket';
// eslint-disable-next-line import/no-extraneous-dependencies
import algosdk from 'algosdk';

// eslint-disable-next-line new-cap
const client = new WebSocket.client();

function processData(connection: WebSocket.connection, data: Buffer) {
  console.log('Decoded data:', algosdk.decodeObj(data));
  connection.sendBytes(data);
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
    if (message.type === 'binary') {
      processData(connection, message.binaryData);
    }
  });
});

client.connect('ws://localhost:8888/');
