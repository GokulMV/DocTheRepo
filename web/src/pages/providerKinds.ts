/** What each kind of LLM provider needs, and where to find it. Drives the "Add provider" form. */

export interface ExtraField {
  key: string;
  label: string;
  placeholder?: string;
  required?: boolean;
  hint?: string;
}

export interface ProviderKind {
  kind: string;
  label: string;
  blurb: string;
  /** API key: required, optional, or not used (cloud credentials of the Hub's host). */
  key: 'required' | 'optional' | 'none';
  keyLabel?: string;
  /** Where to create the key, as numbered steps; the first link opens the right page. */
  keySteps?: { text: string; href?: string }[];
  /** How this kind authenticates when key is 'none'. */
  authNote?: string;
  baseURL: 'required' | 'optional' | 'hidden';
  baseURLLabel?: string;
  baseURLPlaceholder?: string;
  extras?: ExtraField[];
  /** Model prefilled for writing and answering, when one is known to work. */
  defaultModel?: string;
  modelPlaceholder: string;
  /** Embeddings (vector search): supported, with a suggested model. */
  embeddings?: { placeholder: string; default?: string };
  /** Chat features this kind can serve (jev answers decisions only). */
  features?: string[];
}

export const CHAT_FEATURES = ['docgen', 'docgen_fast', 'qa', 'decode', 'triage', 'suggest', 'decide'];

export const PROVIDER_KINDS: ProviderKind[] = [
  {
    kind: 'anthropic',
    label: 'Anthropic (Claude)',
    blurb: 'Claude models through the Claude API.',
    key: 'required',
    keySteps: [
      { text: 'Open the Claude Console → API keys', href: 'https://console.anthropic.com/settings/keys' },
      { text: 'Click “Create key”, name it (e.g. “DocTheRepo Hub”) and copy it. It starts with sk-ant-.' },
      { text: 'Make sure the workspace has billing set up (Settings → Billing).' },
    ],
    baseURL: 'hidden',
    defaultModel: 'claude-sonnet-5-5',
    modelPlaceholder: 'claude-sonnet-5-5',
  },
  {
    kind: 'openai',
    label: 'OpenAI',
    blurb: 'GPT models and embeddings through the OpenAI API.',
    key: 'required',
    keySteps: [
      { text: 'Open the OpenAI platform → API keys', href: 'https://platform.openai.com/api-keys' },
      { text: 'Click “Create new secret key” and copy it. It starts with sk-.' },
    ],
    baseURL: 'hidden',
    extras: [{ key: 'organization', label: 'Organization ID (optional)', placeholder: 'org-…', hint: 'Only if your key belongs to several organizations.' }],
    modelPlaceholder: 'The model name from your OpenAI account, e.g. gpt-5',
    embeddings: { placeholder: 'text-embedding-3-small', default: 'text-embedding-3-small' },
  },
  {
    kind: 'azure_openai',
    label: 'Azure OpenAI',
    blurb: 'OpenAI models deployed in your Azure subscription.',
    key: 'required',
    keySteps: [
      { text: 'Open the Azure portal and your Azure OpenAI resource', href: 'https://portal.azure.com/#browse/Microsoft.CognitiveServices%2Faccounts' },
      { text: 'Go to “Keys and Endpoint”: copy KEY 1 here and the Endpoint below.' },
      { text: 'Your deployment names are under “Model deployments” (Azure AI Foundry).' },
    ],
    baseURL: 'required',
    baseURLLabel: 'Endpoint',
    baseURLPlaceholder: 'https://my-resource.openai.azure.com',
    extras: [
      { key: 'deployment', label: 'Chat deployment name', required: true, placeholder: 'gpt-4o' },
      { key: 'embedding_deployment', label: 'Embedding deployment name (optional)', placeholder: 'text-embedding-3-small' },
      { key: 'api_version', label: 'API version (optional)', placeholder: 'Leave empty for the default' },
    ],
    modelPlaceholder: 'Same as the chat deployment name',
    embeddings: { placeholder: 'Same as the embedding deployment name' },
  },
  {
    kind: 'bedrock',
    label: 'AWS Bedrock',
    blurb: 'Models in your AWS account, through Bedrock.',
    key: 'none',
    authNote: 'Uses the AWS credentials of the machine the Hub runs on (an IAM role, or a profile in ~/.aws). The role needs bedrock:InvokeModel, and model access enabled in the Bedrock console.',
    baseURL: 'hidden',
    extras: [
      { key: 'region', label: 'Region', required: true, placeholder: 'us-east-1' },
      { key: 'profile', label: 'AWS profile (optional)', placeholder: 'default' },
    ],
    modelPlaceholder: 'A Bedrock model or inference-profile ID from the Bedrock console',
    embeddings: { placeholder: 'amazon.titan-embed-text-v2:0' },
  },
  {
    kind: 'vertex',
    label: 'Google Vertex AI',
    blurb: 'Models in your Google Cloud project, through Vertex AI.',
    key: 'none',
    authNote: 'Uses Application Default Credentials: Workload Identity on GKE/Cloud Run, or `gcloud auth application-default login` locally. The account needs the Vertex AI User role.',
    baseURL: 'hidden',
    extras: [
      { key: 'project', label: 'Project ID', required: true, placeholder: 'my-project' },
      { key: 'region', label: 'Region', required: true, placeholder: 'us-east5' },
    ],
    modelPlaceholder: 'A model ID from Vertex AI Model Garden',
    embeddings: { placeholder: 'text-embedding-005' },
  },
  {
    kind: 'ollama',
    label: 'Ollama (local)',
    blurb: 'Open models running on your own machine or server. No key, nothing leaves your network.',
    key: 'none',
    authNote: 'Install Ollama (ollama.com), then pull models, e.g. `ollama pull llama3.1` and `ollama pull nomic-embed-text`.',
    baseURL: 'optional',
    baseURLPlaceholder: 'http://localhost:11434/v1',
    modelPlaceholder: 'llama3.1',
    embeddings: { placeholder: 'nomic-embed-text' },
  },
  {
    kind: 'openai_compat',
    label: 'OpenAI-compatible server',
    blurb: 'vLLM, LiteLLM, LM Studio, or any server that speaks the OpenAI API.',
    key: 'optional',
    keyLabel: 'API key (if the server needs one)',
    baseURL: 'required',
    baseURLPlaceholder: 'https://llm.internal.example/v1',
    modelPlaceholder: 'The model name the server exposes',
    embeddings: { placeholder: 'The embedding model the server exposes' },
  },
  {
    kind: 'external_cli',
    label: 'Agent CLI',
    blurb: 'A headless coding agent (e.g. opencode) that writes docs through the DocGen contract.',
    key: 'optional',
    baseURL: 'hidden',
    extras: [{ key: 'command_template', label: 'Command template', required: true, placeholder: 'opencode run --model {model} …' }],
    modelPlaceholder: 'The model the agent should use',
    features: ['docgen'],
  },
  {
    kind: 'jev',
    label: 'TypeSafe Jev',
    blurb: 'Calibrated yes/no decisions (for example, skip decoding obvious noise). Decisions only.',
    key: 'required',
    baseURL: 'optional',
    modelPlaceholder: 'jev-latest',
    features: ['decide'],
  },
];

export function providerKind(kind: string): ProviderKind | undefined {
  return PROVIDER_KINDS.find((k) => k.kind === kind);
}
