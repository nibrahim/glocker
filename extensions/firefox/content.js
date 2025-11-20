// Page content analysis - will be populated from server
let contentKeywords = ['trigger1', 'trigger2']; // fallback defaults

// Fetch keywords from glocker server
async function fetchKeywords() {
  console.log('Fetching keywords from server...');
  try {
    const response = await fetch('http://127.0.0.1/keywords');
    console.log('Keywords fetch response status:', response.status);
    
    if (response.ok) {
      const data = await response.json();
      console.log('Keywords response data:', data);
      if (data.content_keywords && Array.isArray(data.content_keywords)) {
        contentKeywords = data.content_keywords;
        console.log('Updated Content keywords from server:', contentKeywords);
      } else {
        console.log('No valid content_keywords in response, using defaults');
      }
      return data;
    } else {
      console.log('Keywords fetch failed with status:', response.status);
    }
  } catch (error) {
    console.log('Keywords fetch error:', error);
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
      if (data.content_keywords && Array.isArray(data.content_keywords)) {
        contentKeywords = data.content_keywords;
        console.log('Updated content keywords via SSE:', contentKeywords);
        
        // Re-analyze current page with new keywords
        if (document.readyState === 'complete') {
          console.log('Re-analyzing page content with updated keywords');
          analyzeContent();
        }
      }
    } catch (error) {
      console.log('Failed to parse SSE message:', error);
    }
  };
  
  eventSource.onerror = function(event) {
    console.log('SSE connection error:', event);
    // Connection will automatically retry
  };
  
  // Store reference for cleanup if needed
  window.glockerSSE = eventSource;
}

// Cleanup function for page unload
function cleanup() {
  console.log('Cleaning up glocker extension resources');
  
  // Clear any pending timeouts
  if (window.glockerContentAnalysisTimeout) {
    clearTimeout(window.glockerContentAnalysisTimeout);
  }
  
  // Disconnect observer
  if (window.glockerObserver) {
    window.glockerObserver.disconnect();
  }
  
  // Close SSE connection
  if (window.glockerSSE) {
    window.glockerSSE.close();
  }
}

// Set up cleanup on page unload
window.addEventListener('beforeunload', cleanup);
window.addEventListener('unload', cleanup);

function analyzeContent() {
  console.log('analyzeContent() called for URL:', window.location.href);
  
  // Skip analyzing localhost/127.0.0.1 pages to prevent redirect loops
  if (window.location.hostname === '127.0.0.1' || window.location.hostname === 'localhost') {
    console.log('Skipping localhost analysis to prevent redirect loops');
    return;
  }
  
  const text = document.body ? document.body.textContent.toLowerCase() : '';
  console.log('Analyzing content, text length:', text.length);
  console.log('Current keywords to check:', contentKeywords);
  
  for (let keyword of contentKeywords) {
    console.log('Checking for keyword:', keyword);
    // Use word boundaries to match whole words only
    const regex = new RegExp('\\b' + keyword.toLowerCase().replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '\\b', 'i');
    if (regex.test(text)) {
      console.log('KEYWORD MATCH FOUND:', keyword);
      const reportData = {
        url: window.location.href,
        domain: window.location.hostname,
        trigger: `content-keyword:${keyword}`,
        timestamp: Date.now()
      };
      
      console.log('Sending report:', reportData);
      fetch('http://127.0.0.1/report', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify(reportData)
      }).catch((error) => {
        console.log('Report send failed:', error);
      });
      
      // Redirect to blocked page with reason
      const reason = encodeURIComponent(`Page content contains blocked keyword: "${keyword}"`);
      console.log('Redirecting to blocked page with reason:', reason);
      window.location.replace(`http://127.0.0.1/blocked?reason=${reason}`);
      
      break; // Only report once per page
    }
  }
  console.log('Content analysis complete, no matches found');
}

// Helper function to check if a node contains meaningful text content
function hasTextContent(node) {
  if (node.nodeType === Node.TEXT_NODE) {
    return node.textContent.trim().length > 0;
  }
  if (node.nodeType === Node.ELEMENT_NODE) {
    // Skip script, style, and other non-visible elements
    const tagName = node.tagName ? node.tagName.toLowerCase() : '';
    if (['script', 'style', 'noscript', 'meta', 'link', 'title'].includes(tagName)) {
      return false;
    }
    return node.textContent.trim().length > 0;
  }
  return false;
}

// Helper function to check if mutations contain text changes
function hasTextChanges(mutations) {
  for (let mutation of mutations) {
    // Check for text content changes
    if (mutation.type === 'characterData') {
      if (mutation.target.textContent.trim().length > 0) {
        console.log('Text content changed:', mutation.target.textContent.substring(0, 50) + '...');
        return true;
      }
    }
    
    // Check for new nodes with text content
    if (mutation.type === 'childList') {
      // Check added nodes
      for (let node of mutation.addedNodes) {
        if (hasTextContent(node)) {
          console.log('Text content added via new node:', node.textContent.substring(0, 50) + '...');
          return true;
        }
      }
      
      // Check if removed nodes had significant text (for dynamic content replacement)
      for (let node of mutation.removedNodes) {
        if (hasTextContent(node)) {
          console.log('Text content removed, may indicate dynamic replacement');
          return true;
        }
      }
    }
  }
  return false;
}

// Set up MutationObserver to watch for dynamically loaded content
function setupContentMonitoring() {
  console.log('Setting up content monitoring...');
  
  // Initial content analysis
  analyzeContent();
  
  // Watch for text content changes
  const observer = new MutationObserver((mutations) => {
    console.log('MutationObserver triggered, mutations count:', mutations.length);
    
    // Only analyze if there are actual text changes
    if (hasTextChanges(mutations)) {
      console.log('Text changes detected, scheduling content analysis');
      // Debounce rapid changes - wait a bit before analyzing
      clearTimeout(window.glockerContentAnalysisTimeout);
      window.glockerContentAnalysisTimeout = setTimeout(() => {
        console.log('Executing delayed content analysis');
        analyzeContent();
      }, 300); // Reduced timeout for better responsiveness
    } else {
      console.log('No relevant text changes detected, skipping analysis');
    }
  });
  
  // Start observing changes to the entire document
  const targetNode = document.body || document.documentElement;
  console.log('Starting MutationObserver on:', targetNode.tagName);
  observer.observe(targetNode, {
    childList: true,
    subtree: true,
    characterData: true
  });
  
  // Store observer reference for cleanup
  window.glockerObserver = observer;
  console.log('Content monitoring setup complete');
}

console.log("Content script starting on:", window.location.href);
console.log("Document ready state:", document.readyState);

// Initialize keywords on startup
fetchKeywords().then((data) => {
  console.log('Keywords fetch completed, data:', data);
  
  // Set up SSE connection for real-time updates
  setupSSEConnection();
  
  // Run content analysis after keywords are loaded
  if (document.readyState === 'loading') {
    console.log('Document still loading, waiting for DOMContentLoaded');
    document.addEventListener('DOMContentLoaded', setupContentMonitoring);
  } else {
    console.log('Document already loaded, setting up monitoring immediately');
    setupContentMonitoring();
  }
}).catch((error) => {
  console.log('Keywords fetch failed, using defaults:', error);
  
  // Still set up SSE connection even if initial fetch failed
  setupSSEConnection();
  
  // Still try to analyze with default keywords
  setupContentMonitoring();
});
