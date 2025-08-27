window.addEventListener('DOMContentLoaded', () => {
    const editor = document.getElementById('editor');
    const status = document.getElementById('status');
    const clientID = 'client-' + Math.random().toString(36).substr(2, 9);
    console.log('My Client ID:', clientID);

    // The client maintains its own copy of the CRDT document model.
    let localDoc = [];

    const socket = new WebSocket('ws://localhost:8080/ws');

    socket.onopen = () => {
        status.textContent = 'Connected';
        status.className = 'font-mono text-green-400';
    };

    socket.onmessage = (event) => {
        const op = JSON.parse(event.data);
        if (op.clientID === clientID) {
            // This is an echo of a change we initiated.
            // We need to apply it to keep our model in sync with the server's authoritative version.
        }
        console.log('Received CRDT Op:', op);
        applyOperation(op);
        render(); // Re-render the editor from the local model
    };

    socket.onclose = () => {
        status.textContent = 'Disconnected';
        status.className = 'font-mono text-red-400';
    };
    socket.onerror = () => {
        status.textContent = 'Error';
        status.className = 'font-mono text-red-400';
    };

    editor.addEventListener('input', (e) => {
        // Find the difference between the editor state and our local model
        const editorText = editor.value;
        const modelText = localDoc.map(c => c.value).join('');

        let i = 0;
        while (i < editorText.length && i < modelText.length && editorText[i] === modelText[i]) {
            i++;
        }

        // Deletion
        if (modelText.length > editorText.length) {
            const rawOp = { action: 'raw_delete', clientID, index: i };
            socket.send(JSON.stringify(rawOp));
            // Immediately update local model for responsiveness
            localDoc.splice(i, 1);
        }
        // Insertion
        else if (editorText.length > modelText.length) {
            const insertedChar = editorText[i];
            const rawOp = { action: 'raw_insert', clientID, char: { value: insertedChar }, index: i };
            socket.send(JSON.stringify(rawOp));
            // Immediately update local model for responsiveness
            // This is a "provisional" character without a real ID/Position yet.
            // It will be replaced when the CRDT op comes back from the server.
            localDoc.splice(i, 0, { value: insertedChar, provisional: true });
        }
    });

    function applyOperation(op) {
        // Remove any provisional chars before applying the real op
        localDoc = localDoc.filter(char => !char.provisional);

        if (op.action === 'crdt_insert') {
            const index = findInsertIndex(op.char);
            localDoc.splice(index, 0, op.char);
        } else if (op.action === 'crdt_delete') {
            localDoc = localDoc.filter(char =>
                !(char.id.peerID === op.char.id.peerID && char.id.clock === op.char.id.clock)
            );
        }
    }

    function render() {
        const text = localDoc.map(char => char.value).join('');
        const cursorPos = editor.selectionStart;
        editor.value = text;
        // Restore cursor only if the document has changed, to avoid flicker
        if (document.activeElement === editor) {
             editor.setSelectionRange(cursorPos, cursorPos);
        }
    }
    
    // Helper for CRDT model operations
    function findInsertIndex(char) {
        let low = 0, high = localDoc.length;
        while (low < high) {
            const mid = Math.floor((low + high) / 2);
            if (comparePositions(localDoc[mid].position, char.position) > 0) {
                high = mid;
            } else {
                low = mid + 1;
            }
        }
        return low;
    }
    
    function comparePositions(pos1, pos2) {
        for (let i = 0; i < pos1.length && i < pos2.length; i++) {
            if (pos1[i] < pos2[i]) return -1;
            if (pos1[i] > pos2[i]) return 1;
        }
        if (pos1.length < pos2.length) return -1;
        if (pos1.length > pos2.length) return 1;
        return 0;
    }
});