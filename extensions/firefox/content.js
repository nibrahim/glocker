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
      }
      return data;
    }
  } catch (error) {
    // Failed to fetch keywords from server, using defaults
  }
  return null;
}

function analyzeContent() {
  const text = document.body ? document.body.textContent.toLowerCase() : '';
  
  for (let keyword of contentKeywords) {
    if (text.includes(keyword)) {
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

// Initialize keywords on startup
fetchKeywords().then(() => {
  // Run content analysis after keywords are loaded
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', analyzeContent);
  } else {
    analyzeContent();
  }
}).catch(() => {
  // Still try to analyze with default keywords
  analyzeContent();
});
