document.addEventListener('DOMContentLoaded', function() {
    // Panel toggle button references
    const leftToggle = document.getElementById('leftToggle');
    const rightToggle = document.getElementById('rightToggle');
    const leftPanel = document.getElementById('leftPanel');
    const rightPanel = document.getElementById('rightPanel');
    const dataContainer = document.getElementById('dataContainer');
    const footerMessage = document.getElementById('footerMessage');
    
    // Panel toggle state
    let leftPanelOpen = false;
    let rightPanelOpen = false;
    
    // Left panel toggle
    leftToggle.addEventListener('click', function() {
        toggleLeftPanel();
    });
    
    // Right panel toggle
    rightToggle.addEventListener('click', function() {
        toggleRightPanel();
    });
    
    // Toggle left panel function
    function toggleLeftPanel() {
        leftPanelOpen = !leftPanelOpen;

        //AI says to do this instead:
        leftPanel.classList.toggle('open');
        leftToggle.querySelector('.arrow-icon').classList.toggle('open');
        leftPanel.style.width = leftPanelOpen ? '250px' : '0';


        // Display a message in the footer
        showFooterMessage(leftPanelOpen ? 'Left panel opened' : 'Left panel closed');
    }
    
    // Toggle right panel function
    function toggleRightPanel() {
        rightPanelOpen = !rightPanelOpen;
        rightPanel.style.width = rightPanelOpen ? '250px' : '0';
        rightToggle.querySelector('.hamburger-icon').classList.toggle('open');
        
        // Update data container margin
        if (rightPanelOpen) {
            dataContainer.classList.add('right-open');
        } else {
            dataContainer.classList.remove('right-open');
        }
        
        // Display a message in the footer
        showFooterMessage(rightPanelOpen ? 'Right panel opened' : 'Right panel closed');
    }
    
    // Function to show a temporary message in the footer
    function showFooterMessage(message) {
        footerMessage.textContent = message;
        footerMessage.classList.add('visible');
        
        // Clear the message after 3 seconds
        setTimeout(function() {
            footerMessage.classList.remove('visible');
            
            // Clear the text after the fade-out animation
            setTimeout(function() {
                footerMessage.textContent = '';
            }, 500);
        }, 3000);
    }
    
    // Example function to dynamically add buttons to left panel
    window.addButtonToLeftPanel = function(text, callback) {
        const button = document.createElement('button');
        button.textContent = text;
        button.addEventListener('click', callback);
        document.getElementById('dynamicButtons').appendChild(button);
        
        // Show a message that a button was added
        showFooterMessage(`Added button: ${text}`);
    };
    
    // Handle login and logout (extending existing functionality)
    const loginBtn = document.getElementById('loginBtn');
    const logoutBtn = document.getElementById('logoutBtn');
    const loginContainer = document.getElementById('loginContainer');
    
    loginBtn.addEventListener('click', function(e) {
        e.preventDefault();
        login();
    });
    
    logoutBtn.addEventListener('click', function() {
        logout();
    });
    
    function login() {
        const passcode = document.getElementById('passcode').value;
        
        // This is using the existing fetch logic from the original code
        fetch("/login", {
            method: "POST",
            headers: {
                "Content-Type": "application/json",
            },
            body: JSON.stringify({ "passcode": passcode }),
        })
        .then((response) => response.json())
        .then((data) => {
            token = data.token;
            localStorage.setItem("token", token);
            showData();
            showFooterMessage('Login successful');
        })
        .catch((error) => {
            console.error("Error:", error);
            showFooterMessage('Login failed');
        });
    }
    
    function logout() {
        fetch("/logout", {
            method: "POST",
            headers: {
                Authorization: `Bearer ${token}`,
            },
        })
        .then(() => {
            localStorage.removeItem("token");
            token = null;
            showLoginForm();
            showFooterMessage('Logout successful');
        })
        .catch((error) => {
            console.error("Error:", error);
            showFooterMessage('Logout failed');
        });
    }
    
    function showLoginForm() {
        leftToggle.style.display = 'none';
        rightToggle.style.display = 'none';
        loginContainer.style.display = 'block';
        dataContainer.style.display = 'none';
        
        // Reset panel states
        leftPanel.style.width = '0';
        rightPanel.style.width = '0';
        leftPanelOpen = false;
        rightPanelOpen = false;
    }
    
    function showData() {
        leftToggle.style.display = 'flex';
        rightToggle.style.display = 'flex';
        loginContainer.style.display = 'none';
        dataContainer.style.display = 'block';
    }
    
    // Password visibility toggle
    window.togglePasswordVisibility = function() {
        const passwordField = document.getElementById('passcode');
        const eyeIcon = document.querySelector('.eye-icon');
        
        if (passwordField.type === 'password') {
            passwordField.type = 'text';
            eyeIcon.textContent = '🔒'; // Closed eye
        } else {
            passwordField.type = 'password';
            eyeIcon.textContent = '👁️'; // Open eye
        }
    };
    
    // Check for existing token on page load
    let token = localStorage.getItem('token');
    if (token) {
        showData();
    } else {
        showLoginForm();
    }
    
    // Initialize panels as closed
    leftPanel.style.width = '0';
    rightPanel.style.width = '0';
    
    // Keep the toggle buttons hidden until logged in
    if (!token) {
        leftToggle.style.display = 'none';
        rightToggle.style.display = 'none';
    }
});

// Add tab functionality
function openTab(evt, tabName) {
    var i, tabcontent, tablinks;
    
    // Hide all tab content
    tabcontent = document.getElementsByClassName("tabcontent");
    for (i = 0; i < tabcontent.length; i++) {
        tabcontent[i].style.display = "none";
    }
    
    // Remove the "active" class from all tab buttons
    tablinks = document.getElementsByClassName("tablinks");
    for (i = 0; i < tablinks.length; i++) {
        tablinks[i].className = tablinks[i].className.replace(" active", "");
    }
    
    // Show the current tab, and add an "active" class to the button that opened the tab
    document.getElementById(tabName).style.display = "block";
    evt.currentTarget.className += " active";
}

// Toggle table visibility
function toggleTable(tableId) {
    const table = document.getElementById(tableId);
    table.classList.toggle('hidden');
}

// Close lightbox
function closeLightBox() {
    document.getElementById('lightbox').style.display = 'none';
}
