// Wait until the entire HTML document is loaded.
window.addEventListener('DOMContentLoaded', (event) => {
    console.log('DOM fully loaded and parsed');

    const editor = document.getElementById('editor');
    
    // Create a new WebSocket connection to our server.
    // Note the 'ws://' protocol.
    const socket = new WebSocket('ws://localhost:8080/ws');

    // Event handler for when the connection is successfully opened.
    socket.onopen = (event) => {
        console.log('WebSocket connection opened:', event);
    };

    // Event handler for receiving messages from the server.
    socket.onmessage = (event) => {
        const message = event.data;
        console.log('Message from server:', message);
        // In a real app, you would update the editor here.
        // For our echo test, we just log it.
    };

    // Event handler for when the connection is closed.
    socket.onclose = (event) => {
        console.log('WebSocket connection closed:', event);
    };

    // Event handler for any errors.
    socket.onerror = (error) => {
        console.error('WebSocket error:', error);
    };

    // Add an event listener to our text area.
    // When the user types, it sends the content to the server.
    editor.addEventListener('input', () => {
        const text = editor.value;
        if (socket.readyState === WebSocket.OPEN) {
            socket.send(text);
        }
    });
});