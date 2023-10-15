import WebSocket from 'websocket';
import { pack, unpack } from 'msgpackr';
import algosdk from 'algosdk';
// eslint-disable-next-line new-cap
const client = new WebSocket.client();

function processData(connection: WebSocket.connection, block: any) {
  block.payset = (block.payset as any[])?.filter((t: any) => {
    // decodeSignedTransaction will complain if gh is not set
    t.txn.gh = block.block.gh;
    const sTxn = algosdk.decodeSignedTransaction(pack(t));
    const noteStr = Buffer.from(sTxn.txn.note!).toString();
    console.log(noteStr);

    return noteStr.length > 0;
  });
  connection.sendBytes(pack(block));
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
      processData(connection, unpack(message.binaryData));
    }
  });
});

client.connect('ws://localhost:8888/read');
