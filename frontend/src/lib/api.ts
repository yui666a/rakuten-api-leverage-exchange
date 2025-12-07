const API_BASE_URL = import.meta.env.VITE_API_BASE_URL || 'http://localhost:8080/api';

export interface Market {
  symbol: string;
  last_price: number;
  volume: number;
  change_24h: number;
  high_24h: number;
  low_24h: number;
  updated_at: string;
}

export interface Order {
  id?: string;
  symbol: string;
  side: 'buy' | 'sell';
  type: 'market' | 'limit';
  price: number;
  amount: number;
  status?: string;
  created_at?: string;
  updated_at?: string;
}

export interface Account {
  id: string;
  balance: number;
  currency: string;
  locked_funds: number;
  updated_at: string;
}

class ApiClient {
  private baseURL: string;

  constructor(baseURL: string) {
    this.baseURL = baseURL;
  }

  private async request<T>(endpoint: string, options?: RequestInit): Promise<T> {
    const url = `${this.baseURL}${endpoint}`;
    
    const response = await fetch(url, {
      ...options,
      headers: {
        'Content-Type': 'application/json',
        ...options?.headers,
      },
    });

    if (!response.ok) {
      const error = await response.json().catch(() => ({ error: 'Request failed' }));
      throw new Error(error.error || `HTTP ${response.status}`);
    }

    return response.json();
  }

  // Market endpoints
  async getMarkets(): Promise<Market[]> {
    return this.request<Market[]>('/markets');
  }

  async getMarket(symbol: string): Promise<Market> {
    return this.request<Market>(`/markets/${symbol}`);
  }

  // Order endpoints
  async createOrder(order: Order): Promise<Order> {
    return this.request<Order>('/orders', {
      method: 'POST',
      body: JSON.stringify(order),
    });
  }

  async getOrders(): Promise<Order[]> {
    return this.request<Order[]>('/orders');
  }

  async getOrder(id: string): Promise<Order> {
    return this.request<Order>(`/orders/${id}`);
  }

  async cancelOrder(id: string): Promise<{ message: string }> {
    return this.request<{ message: string }>(`/orders/${id}`, {
      method: 'DELETE',
    });
  }

  // Account endpoints
  async getAccount(id: string): Promise<Account> {
    return this.request<Account>(`/accounts/${id}`);
  }
}

export const apiClient = new ApiClient(API_BASE_URL);
