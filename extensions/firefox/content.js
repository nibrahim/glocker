// Page content analysis - will be populated from server
let contentKeywords = ['trigger1', 'trigger2']; // fallback defaults

// Fetch keywords from glocker server
async function fetchKeywords() {
  try {
    const response = await fetch('http://127.0.0.1/keywords');
    
    if (response.ok) {
      const data = await response.json();
      if (data.content_keywords && Array.isArray(data.content_keywords)) {
        contentKeywords = data.content_keywords;
        console.log('Updated Content keywords from server:', contentKeywords);
      }
      return data;
    }
  } catch (error) {
    // Failed to fetch keywords from server, using defaults
  }
  return null;
}

function analyzeContent() {
  // Skip analyzing localhost/127.0.0.1 pages to prevent redirect loops
  if (window.location.hostname === '127.0.0.1' || window.location.hostname === 'localhost') {
    return;
  }
  
  const text = document.body ? document.body.textContent.toLowerCase() : '';
  
  for (let keyword of contentKeywords) {
    // Use word boundaries to match whole words only
    const regex = new RegExp('\\b' + keyword.toLowerCase().replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '\\b', 'i');
    if (regex.test(text)) {
      const reportData = {
        url: window.location.href,
        domain: window.location.hostname,
        trigger: `content-keyword:${keyword}`,
        timestamp: Date.now()
      };
      
      fetch('http://127.0.0.1/report', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify(reportData)
      }).catch(() => {}); // Ignore failures
      
      // Redirect to blocked page with reason
      const reason = encodeURIComponent(`Page content contains blocked keyword: "${keyword}"`);
      window.location.href = `http://127.0.0.1/blocked?reason=${reason}`;
      
      break; // Only report once per page
    }
  }
}

console.log("Starting");
// Initialize keywords on startup
fetchKeywords().then(() => {
  // Run content analysis after keywords are loaded
    console.log('Starting');    
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', analyzeContent);
  } else {
    analyzeContent();
  }
}).catch(() => {
  // Still try to analyze with default keywords
  analyzeContent();
});
