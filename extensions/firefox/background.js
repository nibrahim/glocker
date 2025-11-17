// URL keyword blocking - will be populated from server
let urlKeywords = ['gambling', 'casino', 'porn', 'xxx']; // fallback defaults

// Fetch keywords from glocker server
async function fetchKeywords() {
  try {
    const response = await fetch('http://127.0.0.1/keywords');
    if (response.ok) {
      const data = await response.json();
      if (data.url_keywords && Array.isArray(data.url_keywords)) {
        urlKeywords = data.url_keywords;
        console.log('Updated URL keywords from server:', urlKeywords);
      }
      return data;
    }
  } catch (error) {
    console.log('Failed to fetch keywords from server, using defaults:', error);
  }
  return null;
}

// Set up SSE connection for real-time keyword updates
function setupSSEConnection() {
  console.log('Setting up SSE connection for keyword updates...');
  
  const eventSource = new EventSource('http://127.0.0.1/keywords-stream');
  
  eventSource.onopen = function(event) {
    console.log('SSE connection opened');
  };
  
  eventSource.onmessage = function(event) {
    console.log('SSE message received:', event.data);
    try {
      const data = JSON.parse(event.data);
      if (data.url_keywords && Array.isArray(data.url_keywords)) {
        urlKeywords = data.url_keywords;
        console.log('Updated URL keywords via SSE:', urlKeywords);
      }
    } catch (error) {
      console.log('Failed to parse SSE message:', error);
    }
  };
  
  eventSource.onerror = function(event) {
    console.log('SSE connection error:', event);
    // Connection will automatically retry
  };
}

// Initialize keywords on startup
fetchKeywords().then(() => {
  // Set up SSE connection for real-time updates
  setupSSEConnection();
}).catch(() => {
  // Still set up SSE connection even if initial fetch failed
  setupSSEConnection();
});

browser.webRequest.onBeforeRequest.addListener(
  function(details) {
    const url = details.url.toLowerCase();
    
    // Skip checking localhost/127.0.0.1 URLs to prevent redirect loops
    if (url.includes('127.0.0.1') || url.includes('localhost')) {
      return;
    }
    
    for (let keyword of urlKeywords) {
      // Use word boundaries to match whole words only
      const regex = new RegExp('\\b' + keyword.toLowerCase().replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '\\b', 'i');
      if (regex.test(url)) {
          console.log("Found ", keyword, " in ", url);
        // Report to glocker
        fetch('http://127.0.0.1/report', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({
            url: details.url,
            trigger: `url-keyword:${keyword}`,
            timestamp: Date.now()
          })
        }).catch(() => {}); // Ignore failures
        
        // Redirect to blocked page with reason
        const reason = encodeURIComponent(`URL contains blocked keyword: "${keyword}"`);
        return {redirectUrl: `http://127.0.0.1/blocked?reason=${reason}`};
      }
    }
  },
  {urls: ["<all_urls>"]},
  ["blocking"]
);
