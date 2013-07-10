#pragma once
#ifndef __CTimedEventScheduler_H__
#define __CTimedEventScheduler_H__

class CTimedEvent;

class CTimedEventScheduler
{
public:
	CTimedEventScheduler();
	~CTimedEventScheduler();

	void ScheduleEvent(CTimedEvent oEvent);
	bool CheckEventById(u_int nId);
	void RemoveEventById(u_int nId);
	void RemoveAllEvents();

private:
	GLFWthread		m_oSchedulerThread;
	volatile bool	m_bSchedulerThreadRun;

	GLFWmutex		m_oSchedulerMutex;

	std::multiset<CTimedEvent>	m_oEvents;

	static void GLFWCALL SchedulerThread(void * pArgument);
};

#endif // __CTimedEventScheduler_H__
