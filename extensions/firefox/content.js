// Debug function that creates visible output
function debugLog(message) {
  console.log('[GLOCKER CONTENT] ' + message);
  
  // Also create a visible debug element on the page for testing
  const debugDiv = document.getElementById('glocker-debug') || (() => {
    const div = document.createElement('div');
    div.id = 'glocker-debug';
    div.style.cssText = 'position:fixed;top:0;right:0;background:red;color:white;padding:5px;z-index:9999;max-width:300px;font-size:12px;';
    document.documentElement.appendChild(div);
    return div;
  })();
  debugDiv.innerHTML += message + '<br>';
}

debugLog('Script loaded, starting execution');

// Page content analysis - will be populated from server
let contentKeywords = ['trigger1', 'trigger2']; // fallback defaults

// Fetch keywords from glocker server
async function fetchKeywords() {
  debugLog('fetchKeywords() called');
  try {
    debugLog('Making fetch request to http://127.0.0.1/keywords');
    const response = await fetch('http://127.0.0.1/keywords');
    debugLog('Fetch response received: ' + response.status + ' ' + response.ok);
    
    if (response.ok) {
      const data = await response.json();
      debugLog('JSON data parsed: ' + JSON.stringify(data));
      if (data.content_keywords && Array.isArray(data.content_keywords)) {
        contentKeywords = data.content_keywords;
        debugLog('Updated content keywords from server: ' + JSON.stringify(contentKeywords));
      }
      debugLog('fetchKeywords() returning data');
      return data;
    } else {
      debugLog('Response not ok, status: ' + response.status);
    }
  } catch (error) {
    debugLog('Failed to fetch keywords from server, using defaults: ' + error);
  }
  debugLog('fetchKeywords() returning null');
  return null;
}

function analyzeContent() {
  debugLog('analyzeContent() called');
  debugLog('Current URL: ' + window.location.href);
  debugLog('Document ready state: ' + document.readyState);
  debugLog('Document body exists: ' + !!document.body);
  
  const text = document.body ? document.body.textContent.toLowerCase() : '';
  debugLog('Text length: ' + text.length);
  debugLog('Using keywords: ' + JSON.stringify(contentKeywords));
  
  for (let keyword of contentKeywords) {
    debugLog('Checking for keyword: ' + keyword);
    if (text.includes(keyword)) {
      debugLog('MATCH FOUND! Keyword: ' + keyword + ' in BODY: ' + window.location.href);
      
      const reportData = {
        url: window.location.href,
        domain: window.location.hostname,
        trigger: `content-keyword:${keyword}`,
        timestamp: Date.now()
      };
      debugLog('Sending report: ' + JSON.stringify(reportData));
      
      fetch('http://127.0.0.1/report', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify(reportData)
      }).then(response => {
        debugLog('Report sent successfully, status: ' + response.status);
      }).catch(error => {
        debugLog('Failed to send report: ' + error);
      });
      
      break; // Only report once per page
    }
  }
  debugLog('analyzeContent() completed');
}

// Initialize keywords on startup
debugLog('Starting initialization');
debugLog('Initial document ready state: ' + document.readyState);

fetchKeywords().then((result) => {
  debugLog('fetchKeywords() promise resolved with: ' + JSON.stringify(result));
  debugLog('Current contentKeywords: ' + JSON.stringify(contentKeywords));
  
  // Run content analysis after keywords are loaded
  debugLog('Checking document ready state: ' + document.readyState);
  if (document.readyState === 'loading') {
    debugLog('Document still loading, adding DOMContentLoaded listener');
    document.addEventListener('DOMContentLoaded', () => {
      debugLog('DOMContentLoaded event fired');
      analyzeContent();
    });
  } else {
    debugLog('Document already loaded, running analyzeContent immediately');
    analyzeContent();
  }
}).catch((error) => {
  debugLog('fetchKeywords() promise rejected: ' + error);
  // Still try to analyze with default keywords
  debugLog('Running analyzeContent with default keywords');
  analyzeContent();
});

debugLog('Initialization setup complete');
