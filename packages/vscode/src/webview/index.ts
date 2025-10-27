/// <reference lib="dom" />

import Sidebar from './Sidebar.svelte';
import Timeline from './views/Timeline.svelte';
import Repository from './views/Repository.svelte';
import Notifications from './views/Notifications.svelte';
import Search from './views/Search.svelte';
import Explore from './views/Explore.svelte';
import Settings from './views/Settings.svelte';
import CreatePost from './views/CreatePost.svelte';
import Post from './views/Post.svelte';
import List from './views/List.svelte';
import Welcome from './views/Welcome.svelte';
import { webLog } from './utils/weblog';

declare global {
  interface Window {
    viewType?: string;
  }
}

// Get view type from window object or determine from DOM
const viewType = window.viewType || 'sidebar';

// Test frontend logging
webLog('info', 'GitSocial frontend starting with view type:', viewType);

// Route to appropriate component
let _app: unknown;

// Handle sidebar separately since it uses a different root element
if (viewType === 'sidebar') {
  const sidebarElement = document.getElementById('sidebar');
  if (!sidebarElement) {
    throw new Error('Sidebar element not found');
  }
  _app = new Sidebar({ target: sidebarElement });
} else {
  // All other views use the #app element
  const appElement = document.getElementById('app');
  if (!appElement) {
    throw new Error('App element not found');
  }

  switch (viewType) {
  case 'timeline':
    _app = new Timeline({ target: appElement });
    break;
  case 'repository':
    _app = new Repository({ target: appElement });
    break;
  case 'notifications':
    _app = new Notifications({ target: appElement });
    break;
  case 'search':
    _app = new Search({ target: appElement });
    break;
  case 'explore':
    _app = new Explore({ target: appElement });
    break;
  case 'settings':
    _app = new Settings({ target: appElement });
    break;
  case 'createPost':
    _app = new CreatePost({ target: appElement });
    break;
  case 'viewPost':
    _app = new Post({ target: appElement });
    break;
  case 'viewList':
    _app = new List({ target: appElement });
    break;
  case 'welcome':
    _app = new Welcome({ target: appElement });
    break;
  default:
    webLog('error', 'Unknown view type:', viewType);
    // Fallback to welcome view for unknown types
    _app = new Welcome({ target: appElement });
  }
}
