window.addEventListener('DOMContentLoaded', () => {
    const editor = document.getElementById('editor');
    const status = document.getElementById('status');
    const clientID = 'client-' + Math.random().toString(36).substr(2, 9);
    console.log('My Client ID:', clientID);

    // The client's source of truth for the document.
    let localDoc = [];

    const socket = new WebSocket('ws://localhost:8080/ws');

    socket.onopen = () => {
        status.textContent = 'Connected';
        status.className = 'font-mono text-green-400';
    };

    socket.onmessage = (event) => {
        const op = JSON.parse(event.data);
        
        // FIX #1: The simplest way to handle echoes is to ignore ops from ourselves.
        // The server will be the source of truth, even for our own changes.
        if (op.clientID === clientID) {
            return; 
        }

        console.log('Received remote Op:', op);
        applyOperation(op);
    };

    socket.onclose = () => { status.textContent = 'Disconnected'; status.className = 'font-mono text-red-400'; };
    socket.onerror = () => { status.textContent = 'Error'; status.className = 'font-mono text-red-400'; };

    editor.addEventListener('input', (e) => {
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
            // Apply change locally for responsiveness
            localDoc.splice(i, modelText.length - editorText.length);
        }
        // Insertion
        else if (editorText.length > modelText.length) {
            const insertedText = editorText.slice(i, i + editorText.length - modelText.length);
            // Send each character as a separate operation
            insertedText.split('').forEach((char, offset) => {
                const rawOp = { action: 'raw_insert', clientID, char: { value: char }, index: i + offset };
                socket.send(JSON.stringify(rawOp));
            });
            // Apply change locally for responsiveness
            const newChars = insertedText.split('').map(c => ({ value: c }));
            localDoc.splice(i, 0, ...newChars);
        }
    });

    function applyOperation(op) {
        // FIX #2: Smarter cursor management
        const preChangeCursor = editor.selectionStart;
        const textBeforeCursor = editor.value.substring(0, preChangeCursor);
        let charsBeforeCursor = 0;
        for (let char of localDoc) {
            if (textBeforeCursor.includes(char.value)) {
                charsBeforeCursor++;
            }
        }

        // FIX #1 (continued): Make the operation idempotent on the client
        const alreadyExists = localDoc.some(char => 
            char.id && op.char.id && 
            char.id.peerID === op.char.id.peerID && 
            char.id.clock === op.char.id.clock
        );

        if (op.action === 'crdt_insert') {
            if (alreadyExists) return; // Don't apply the same op twice
            const index = findInsertIndex(op.char);
            localDoc.splice(index, 0, op.char);
        } else if (op.action === 'crdt_delete') {
            localDoc = localDoc.filter(char =>
                !(char.id.peerID === op.char.id.peerID && char.id.clock === op.char.id.clock)
            );
        }

        render(preChangeCursor);
    }
    
    function render(originalCursorPos) {
        const text = localDoc.map(char => char.value).join('');
        
        // FIX #2 (continued): Calculate new cursor position
        let newCursorPos = originalCursorPos;
        const oldText = editor.value;
        if (text.length < oldText.length) { // Deletion
            // This is a simple approximation, more complex logic is needed for perfect cursor on remote delete
        } else if (text.length > oldText.length) { // Insertion
            // Find what was inserted before our original cursor position to adjust it
            let diffIndex = 0;
            while(diffIndex < oldText.length && oldText[diffIndex] === text[diffIndex]) {
                diffIndex++;
            }
            if (diffIndex < originalCursorPos) {
                newCursorPos += text.length - oldText.length;
            }
        }
        
        editor.value = text;
        if (document.activeElement === editor) {
            editor.setSelectionRange(newCursorPos, newCursorPos);
        }
    }

    // Helper for CRDT model operations (unchanged)
    function findInsertIndex(char) {
        let low = 0, high = localDoc.length;
        while (low < high) {
            const mid = Math.floor((low + high) / 2);
            if (localDoc[mid].provisional || comparePositions(localDoc[mid].position, char.position) > 0) {
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