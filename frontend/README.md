# BDS Platform Console

The frontend console for the BDS Kubernetes Application Platform.

## Tech Stack

- **Framework:** Next.js 15 with App Router
- **Language:** TypeScript
- **Styling:** Tailwind CSS
- **Components:** shadcn/ui (Radix UI primitives)
- **State Management:** TanStack Query v5
- **Forms:** React Hook Form with Zod validation
- **Icons:** Lucide React

## Getting Started

### Prerequisites

- Node.js 18+
- npm or yarn

### Installation

```bash
# Install dependencies
npm install

# Copy environment file
cp .env.example .env.local

# Update the API URL in .env.local
# NEXT_PUBLIC_API_URL=http://localhost:8080
```

### Development

```bash
# Start development server
npm run dev

# Open http://localhost:3000
```

### Production

```bash
# Build for production
npm run build

# Start production server
npm start
```

## Project Structure

```
frontend/
├── src/
│   ├── app/                    # Next.js App Router pages
│   │   ├── (auth)/            # Auth pages (login, signup)
│   │   ├── (dashboard)/       # Dashboard pages
│   │   │   ├── organizations/ # Organization management
│   │   │   └── [orgSlug]/     # Org-scoped pages
│   │   │       ├── projects/  # Projects
│   │   │       ├── clusters/  # Clusters
│   │   │       ├── deployments/ # Deployments
│   │   │       ├── audit/     # Audit logs
│   │   │       └── settings/  # Settings
│   │   ├── layout.tsx         # Root layout
│   │   └── page.tsx           # Home redirect
│   │
│   ├── components/
│   │   ├── ui/                # shadcn/ui components
│   │   ├── layout/            # Header, Sidebar
│   │   ├── shared/            # Reusable components
│   │   ├── applications/      # Application components
│   │   ├── clusters/          # Cluster components
│   │   ├── deployments/       # Deployment components
│   │   ├── organizations/     # Organization components
│   │   ├── projects/          # Project components
│   │   └── secrets/           # Secret components
│   │
│   ├── hooks/                 # Custom React hooks
│   │   ├── use-toast.ts       # Toast notifications
│   │   ├── use-organizations.ts
│   │   ├── use-projects.ts
│   │   ├── use-clusters.ts
│   │   ├── use-secrets.ts
│   │   ├── use-applications.ts
│   │   ├── use-deployments.ts
│   │   └── use-audit.ts
│   │
│   ├── lib/
│   │   ├── api/               # API client and endpoints
│   │   │   ├── client.ts      # HTTP client with auth
│   │   │   ├── auth.ts        # Auth API
│   │   │   ├── organizations.ts
│   │   │   ├── projects.ts
│   │   │   ├── clusters.ts
│   │   │   ├── secrets.ts
│   │   │   ├── applications.ts
│   │   │   ├── deployments.ts
│   │   │   └── audit.ts
│   │   └── utils.ts           # Utility functions
│   │
│   ├── providers/             # React context providers
│   │   ├── query-provider.tsx # TanStack Query
│   │   ├── auth-provider.tsx  # Authentication
│   │   └── organization-provider.tsx
│   │
│   └── types/
│       └── api.ts             # TypeScript types
│
├── package.json
├── tailwind.config.ts
├── tsconfig.json
└── next.config.ts
```

## Features

### Authentication
- Login/Signup with email and password
- JWT token management with automatic refresh
- Route protection for authenticated pages

### Organization Management
- Create and manage organizations
- Invite members with role-based access
- Member management (admin, member, viewer roles)

### Project Management
- Create projects within organizations
- Organize applications and secrets
- Project-scoped resource management

### Cluster Management
- Register Kubernetes clusters
- Generate registration tokens
- View cluster health and heartbeats
- Helm installation commands

### Secrets Management
- Create encrypted secrets
- Update secret values (never displayed)
- Project-scoped secret organization

### Application Management
- Create applications
- Docker image configuration
- Runtime type selection

### Deployment Management
- Create deployments with resource limits
- Environment variable configuration
- View release history
- Rollback to previous releases

### Audit Logs
- Filter by resource type and action
- View action details and timestamps
- Actor tracking

## API Integration

The API client (`src/lib/api/client.ts`) handles:
- Automatic token refresh on 401 responses
- Request/response error handling
- Type-safe API calls

## State Management

TanStack Query is used for:
- Server state caching
- Automatic refetching
- Optimistic updates
- Loading and error states

## Form Handling

React Hook Form with Zod validation provides:
- Type-safe form definitions
- Client-side validation
- Error message display
- Loading state handling

## Component Library

shadcn/ui components are used for:
- Consistent design system
- Accessible components
- Customizable with Tailwind

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `NEXT_PUBLIC_API_URL` | Backend API URL | `http://localhost:8080` |
| `NEXT_PUBLIC_APP_URL` | Frontend URL | `http://localhost:3000` |

## Scripts

```bash
npm run dev        # Start development server
npm run build      # Build for production
npm run start      # Start production server
npm run lint       # Run ESLint
npm run type-check # Run TypeScript check
```

## License

Proprietary - BDS Platform
