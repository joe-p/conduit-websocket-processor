import WebSocket from 'websocket';
import { decode } from '@msgpack/msgpack';

// eslint-disable-next-line new-cap
const client = new WebSocket.client();

function processData(connection: WebSocket.connection, data: Buffer) {
  const signedTxns = decode(data) as any

  console.log('decoded signedTxns', signedTxns)
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
