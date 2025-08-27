window.addEventListener('DOMContentLoaded', (event) => {
    const editor = document.getElementById('editor');
    const status = document.getElementById('status'); // Get the status element
    let lastText = ''; // Keep track of the last known text

    const socket = new WebSocket('ws://localhost:8080/ws');

    socket.onopen = (event) => {
        console.log('WebSocket connection opened.');
        status.textContent = 'Connected';
        status.className = 'font-mono text-green-400';
    };

    socket.onmessage = (event) => {
        const op = JSON.parse(event.data);
        console.log('Received Op:', op);
        applyOperation(op);
    };
    
    socket.onclose = (event) => {
        console.log('WebSocket connection closed.');
        status.textContent = 'Disconnected';
        status.className = 'font-mono text-red-400';
    };

    socket.onerror = (error) => {
        console.error('WebSocket error:', error);
        status.textContent = 'Error';
        status.className = 'font-mono text-red-400';
    };

    editor.addEventListener('input', (e) => {
        const currentText = editor.value;
        const op = diff(lastText, currentText);
        
        if (op && socket.readyState === WebSocket.OPEN) {
            socket.send(JSON.stringify(op));
        }

        lastText = currentText;
    });

    function diff(oldStr, newStr) {
        let i = 0;
        while (i < oldStr.length && i < newStr.length && oldStr[i] === newStr[i]) {
            i++;
        }

        if (newStr.length > oldStr.length) {
            return { action: 'insert', char: newStr[i], index: i };
        } else if (newStr.length < oldStr.length) {
            return { action: 'delete', char: oldStr[i], index: i };
        }
        return null;
    }

    function applyOperation(op) {
        const currentText = editor.value;
        let newText;

        if (op.action === 'insert') {
            newText = currentText.slice(0, op.index) + op.char + currentText.slice(op.index);
        } else if (op.action === 'delete') {
            newText = currentText.slice(0, op.index) + currentText.slice(op.index + 1);
        }

        if (newText !== undefined) {
            lastText = newText;
            editor.value = newText;
        }
    }
});