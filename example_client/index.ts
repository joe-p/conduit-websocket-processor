import WebSocket from 'websocket';
import { decode, encode } from '@msgpack/msgpack';
import algosdk from 'algosdk';

// eslint-disable-next-line new-cap
const client = new WebSocket.client();

function processData(connection: WebSocket.connection, data: Buffer) {
  const signedTxns = decode(data) as any[];

  signedTxns.forEach((stxn) => {
    // gh isn't set on transactions so we must manually set it to avoid an error being thrown by decodeSignedTransaction
    stxn.txn.gh = '';

    // encode the stxn as msgpack, then decode with decodeSignedTransaction to get SDK object
    const decodedStxn = algosdk.decodeSignedTransaction(encode(stxn));
    console.log(Buffer.from(decodedStxn.txn.note!).toString());
  });
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
