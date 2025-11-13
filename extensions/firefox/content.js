console.log('[GLOCKER CONTENT] Script loaded, starting execution');

// Page content analysis - will be populated from server
let contentKeywords = ['trigger1', 'trigger2']; // fallback defaults

// Fetch keywords from glocker server
async function fetchKeywords() {
  console.log('[GLOCKER CONTENT] fetchKeywords() called');
  try {
    console.log('[GLOCKER CONTENT] Making fetch request to http://127.0.0.1/keywords');
    const response = await fetch('http://127.0.0.1/keywords');
    console.log('[GLOCKER CONTENT] Fetch response received:', response.status, response.ok);
    
    if (response.ok) {
      const data = await response.json();
      console.log('[GLOCKER CONTENT] JSON data parsed:', data);
      if (data.content_keywords && Array.isArray(data.content_keywords)) {
        contentKeywords = data.content_keywords;
        console.log('[GLOCKER CONTENT] Updated content keywords from server:', contentKeywords);
      }
      console.log('[GLOCKER CONTENT] fetchKeywords() returning data');
      return data;
    } else {
      console.log('[GLOCKER CONTENT] Response not ok, status:', response.status);
    }
  } catch (error) {
    console.log('[GLOCKER CONTENT] Failed to fetch keywords from server, using defaults:', error);
  }
  console.log('[GLOCKER CONTENT] fetchKeywords() returning null');
  return null;
}

function analyzeContent() {
  console.log('[GLOCKER CONTENT] analyzeContent() called');
  console.log('[GLOCKER CONTENT] Current URL:', window.location.href);
  console.log('[GLOCKER CONTENT] Document ready state:', document.readyState);
  console.log('[GLOCKER CONTENT] Document body exists:', !!document.body);
  
  const text = document.body ? document.body.textContent.toLowerCase() : '';
  console.log('[GLOCKER CONTENT] Text length:', text.length);
  console.log('[GLOCKER CONTENT] Using keywords:', contentKeywords);
  
  for (let keyword of contentKeywords) {
    console.log('[GLOCKER CONTENT] Checking for keyword:', keyword);
    if (text.includes(keyword)) {
      console.log('[GLOCKER CONTENT] MATCH FOUND! Keyword:', keyword, 'in URL:', window.location.href);
      
      const reportData = {
        url: window.location.href,
        domain: window.location.hostname,
        trigger: `content-keyword:${keyword}`,
        timestamp: Date.now()
      };
      console.log('[GLOCKER CONTENT] Sending report:', reportData);
      
      fetch('http://127.0.0.1/report', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify(reportData)
      }).then(response => {
        console.log('[GLOCKER CONTENT] Report sent successfully, status:', response.status);
      }).catch(error => {
        console.log('[GLOCKER CONTENT] Failed to send report:', error);
      });
      
      break; // Only report once per page
    }
  }
  console.log('[GLOCKER CONTENT] analyzeContent() completed');
}

// Initialize keywords on startup
console.log('[GLOCKER CONTENT] Starting initialization');
console.log('[GLOCKER CONTENT] Initial document ready state:', document.readyState);

fetchKeywords().then((result) => {
  console.log('[GLOCKER CONTENT] fetchKeywords() promise resolved with:', result);
  console.log('[GLOCKER CONTENT] Current contentKeywords:', contentKeywords);
  
  // Run content analysis after keywords are loaded
  console.log('[GLOCKER CONTENT] Checking document ready state:', document.readyState);
  if (document.readyState === 'loading') {
    console.log('[GLOCKER CONTENT] Document still loading, adding DOMContentLoaded listener');
    document.addEventListener('DOMContentLoaded', () => {
      console.log('[GLOCKER CONTENT] DOMContentLoaded event fired');
      analyzeContent();
    });
  } else {
    console.log('[GLOCKER CONTENT] Document already loaded, running analyzeContent immediately');
    analyzeContent();
  }
}).catch((error) => {
  console.log('[GLOCKER CONTENT] fetchKeywords() promise rejected:', error);
  // Still try to analyze with default keywords
  console.log('[GLOCKER CONTENT] Running analyzeContent with default keywords');
  analyzeContent();
});

console.log('[GLOCKER CONTENT] Initialization setup complete');
